// SPDX-License-Identifier: AGPL-3.0-or-later

package todoist

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/lightheaded/teha/id"
	"github.com/lightheaded/teha/internal/store"
)

// DefaultBatch is the largest number of commands in one store.Apply call. It
// matches the Todoist limit of 100 commands per request, which keeps one
// transaction small enough to read in a log.
const DefaultBatch = 100

// SourcePrefix marks a row that came from Todoist. The importer writes
// "todoist:<id>" into source_ref, so a later run finds the same row and an old
// Todoist link still resolves.
const SourcePrefix = "todoist:"

// Options steers one import.
type Options struct {
	// DryRun builds every command and writes nothing.
	DryRun bool
	// Batch overrides DefaultBatch.
	Batch int
	// Requests is the number of HTTP requests that the read cost, for the
	// summary only.
	Requests int
}

// Summary counts what one import did. Every field is for a person to read.
type Summary struct {
	Projects         int
	ProjectsPresent  int
	ProjectsArchived int
	Labels           int
	LabelsPresent    int
	Tasks            int
	TasksPresent     int
	SubTasks         int
	Completed        int
	Recurring        int
	RecurrenceFailed int
	RecurrenceKept   []string
	SectionsFolded   int
	CommentsFolded   int
	ProjectComments  int
	FiltersSkipped   int
	FiltersKept      []string
	Resumed          int
	Reparented       int
	ArchivedTasks    int
	Commands         int
	Failed           int
	Errors           []string
	Requests         int
	Version          int64
	Elapsed          time.Duration
	DryRun           bool
}

// Import maps a sync payload into the store. A nil store is allowed only with
// Options.DryRun, which is how a dry run works without a database file.
func Import(st *store.Store, data *Sync, opt Options) (Summary, error) {
	start := time.Now()
	sum := Summary{DryRun: opt.DryRun, Requests: opt.Requests}
	if data == nil {
		return sum, fmt.Errorf("no sync payload")
	}
	if st == nil && !opt.DryRun {
		return sum, fmt.Errorf("a real import needs an open store")
	}
	batch := opt.Batch
	if batch <= 0 {
		batch = DefaultBatch
	}

	known, err := readExisting(st)
	if err != nil {
		return sum, err
	}

	cmds, err := buildCommands(data, known, &sum)
	if err != nil {
		return sum, err
	}
	sum.Commands = len(cmds)

	for _, info := range data.CompletedInfo {
		sum.ArchivedTasks += info.CompletedItems
	}

	if !opt.DryRun {
		for from := 0; from < len(cmds); from += batch {
			to := from + batch
			if to > len(cmds) {
				to = len(cmds)
			}
			v, results, err := st.Apply(cmds[from:to])
			if err != nil {
				return sum, err
			}
			sum.Version = v
			for _, r := range results {
				if r.OK {
					continue
				}
				sum.Failed++
				if len(sum.Errors) < 10 {
					sum.Errors = append(sum.Errors, r.Error)
				}
			}
		}
	}
	if !opt.DryRun {
		// A run that wrote nothing still reports the version of the database,
		// so a person sees the same number after every repeat run.
		v, err := st.Version()
		if err != nil {
			return sum, err
		}
		sum.Version = v
	}
	sum.Elapsed = time.Since(start)
	return sum, nil
}

// existing holds the rows that the store already has, so a second run reuses
// the same ids instead of writing a second copy.
type existing struct {
	projectByName map[string]string
	labelByName   map[string]string
	taskBySource  map[string]string
	// taskRowBySource holds the row itself, so a resume can see what an
	// interrupted run did not finish.
	taskRowBySource map[string]store.Task
}

func readExisting(st *store.Store) (*existing, error) {
	out := &existing{
		projectByName:   map[string]string{},
		labelByName:     map[string]string{},
		taskBySource:    map[string]string{},
		taskRowBySource: map[string]store.Task{},
	}
	if st == nil {
		return out, nil
	}
	projects, err := st.Projects()
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		out.projectByName[strings.ToLower(p.Name)] = p.ID
	}
	labels, err := st.Labels()
	if err != nil {
		return nil, err
	}
	for _, l := range labels {
		out.labelByName[strings.ToLower(l.Name)] = l.ID
	}
	// The project and label tables carry no source_ref column, so a name is the
	// only key for them. A task has source_ref, which is the exact key.
	delta, err := st.Pull(0)
	if err != nil {
		return nil, err
	}
	for _, t := range delta.Tasks {
		if t.SourceRef != nil && strings.HasPrefix(*t.SourceRef, SourcePrefix) {
			out.taskBySource[*t.SourceRef] = t.ID
			out.taskRowBySource[*t.SourceRef] = t
		}
	}
	return out, nil
}

func buildCommands(data *Sync, known *existing, sum *Summary) ([]store.Command, error) {
	var cmds []store.Command

	projectID, projectCmds := mapProjects(data, known, sum)
	cmds = append(cmds, projectCmds...)

	labelCmds := mapLabels(data, known, sum)
	cmds = append(cmds, labelCmds...)

	taskCmds, err := mapTasks(data, known, sum, projectID)
	if err != nil {
		return nil, err
	}
	cmds = append(cmds, taskCmds...)

	// A saved filter has no table in our schema, so the importer writes none.
	// It records every name and every query instead, and the summary prints
	// them, so a person can type them back in and nothing is lost in silence.
	for _, f := range data.Filters {
		if bool(f.IsDeleted) {
			continue
		}
		sum.FiltersSkipped++
		sum.FiltersKept = append(sum.FiltersKept, strings.TrimSpace(f.Name)+": "+strings.TrimSpace(f.Query))
	}
	return cmds, nil
}

// mapProjects returns the Todoist id to local id map and the add commands. The
// order is parents before children, so a parent_id always points at a row that
// exists.
func mapProjects(data *Sync, known *existing, sum *Summary) (map[string]string, []store.Command) {
	local := map[string]string{}
	live := make([]Project, 0, len(data.Projects))
	for _, p := range data.Projects {
		if bool(p.IsDeleted) {
			continue
		}
		if bool(p.IsArchived) {
			// Our schema has no archive flag. An archived project keeps no
			// active task in a full sync, so the importer counts it and moves on.
			sum.ProjectsArchived++
			continue
		}
		live = append(live, p)
	}

	// The inbox is fixed in our store, so the Todoist inbox maps onto it.
	for _, p := range live {
		if p.IsInbox() {
			local[p.ID.String()] = store.InboxID
		}
	}

	byParent := map[string][]Project{}
	for _, p := range live {
		if p.IsInbox() {
			continue
		}
		byParent[p.ParentID.String()] = append(byParent[p.ParentID.String()], p)
	}
	for key := range byParent {
		list := byParent[key]
		sort.SliceStable(list, func(i, j int) bool { return list[i].ChildOrder < list[j].ChildOrder })
		byParent[key] = list
	}

	var cmds []store.Command
	// A breadth-first walk from the roots keeps every parent ahead of its
	// children. A project whose parent is missing becomes a root.
	roots := append([]Project{}, byParent[""]...)
	for _, p := range live {
		if p.IsInbox() || p.ParentID == "" {
			continue
		}
		if !hasProject(live, p.ParentID) {
			roots = append(roots, p)
			sum.Reparented++
		}
	}
	queue := roots
	seen := map[string]bool{}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if seen[p.ID.String()] {
			continue
		}
		seen[p.ID.String()] = true

		name := strings.TrimSpace(p.Name)
		if reuse, ok := known.projectByName[strings.ToLower(name)]; ok {
			local[p.ID.String()] = reuse
			sum.ProjectsPresent++
		} else {
			newID := id.New("p")
			local[p.ID.String()] = newID
			known.projectByName[strings.ToLower(name)] = newID
			args := store.ProjectArgs{ID: newID, Name: strptr(name), OrderKey: strptr(orderKey(p.ChildOrder))}
			if p.Color != "" {
				args.Color = strptr(p.Color)
			}
			if parent, ok := local[p.ParentID.String()]; ok && parent != newID {
				args.ParentID = strptr(parent)
			}
			cmds = append(cmds, command("project_add", "import-project-"+p.ID.String(), args))
			sum.Projects++
		}
		queue = append(queue, byParent[p.ID.String()]...)
	}
	return local, cmds
}

func hasProject(list []Project, id ID) bool {
	for _, p := range list {
		if p.ID == id {
			return true
		}
	}
	return false
}

func mapLabels(data *Sync, known *existing, sum *Summary) []store.Command {
	var cmds []store.Command
	list := append([]Label{}, data.Labels...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].ItemOrder < list[j].ItemOrder })
	for _, l := range list {
		if bool(l.IsDeleted) {
			continue
		}
		name := strings.TrimSpace(l.Name)
		if name == "" {
			continue
		}
		if _, ok := known.labelByName[strings.ToLower(name)]; ok {
			sum.LabelsPresent++
			continue
		}
		newID := id.New("l")
		known.labelByName[strings.ToLower(name)] = newID
		args := store.LabelArgs{ID: newID, Name: strptr(name)}
		if l.Color != "" {
			args.Color = strptr(l.Color)
		}
		cmds = append(cmds, command("label_add", "import-label-"+l.ID.String(), args))
		sum.Labels++
	}
	return cmds
}

func mapTasks(data *Sync, known *existing, sum *Summary, projectOf map[string]string) ([]store.Command, error) {
	sections := map[string]string{}
	for _, s := range data.Sections {
		if bool(s.IsDeleted) {
			continue
		}
		sections[s.ID.String()] = strings.TrimSpace(s.Name)
	}

	comments := map[string][]Note{}
	for _, n := range data.Notes {
		if bool(n.IsDeleted) {
			continue
		}
		task := n.Task().String()
		if task == "" {
			sum.ProjectComments++
			continue
		}
		comments[task] = append(comments[task], n)
	}
	for _, n := range data.ProjectNotes {
		if !bool(n.IsDeleted) {
			sum.ProjectComments++
		}
	}
	for key := range comments {
		list := comments[key]
		sort.SliceStable(list, func(i, j int) bool { return list[i].PostedAt < list[j].PostedAt })
		comments[key] = list
	}

	live := make([]Item, 0, len(data.Items))
	index := map[string]Item{}
	for _, it := range data.Items {
		if bool(it.IsDeleted) {
			continue
		}
		live = append(live, it)
		index[it.ID.String()] = it
	}

	// depth orders the writes: a root task first, then every level of
	// sub-task, so parent_id always points at a row that exists.
	depth := map[string]int{}
	var depthOf func(it Item, guard int) int
	depthOf = func(it Item, guard int) int {
		key := it.ID.String()
		if d, ok := depth[key]; ok {
			return d
		}
		if it.ParentID == "" || guard > 20 {
			depth[key] = 0
			return 0
		}
		parent, ok := index[it.ParentID.String()]
		if !ok {
			depth[key] = 0
			return 0
		}
		d := depthOf(parent, guard+1) + 1
		depth[key] = d
		return d
	}
	for _, it := range live {
		depthOf(it, 0)
	}

	sort.SliceStable(live, func(i, j int) bool {
		di, dj := depth[live[i].ID.String()], depth[live[j].ID.String()]
		if di != dj {
			return di < dj
		}
		return live[i].ChildOrder < live[j].ChildOrder
	})

	localID := map[string]string{}
	var cmds []store.Command
	for _, it := range live {
		key := it.ID.String()
		source := SourcePrefix + key
		if reuse, ok := known.taskBySource[source]; ok {
			// The row is already in the store, from an earlier run of this same
			// import. Keep the id so a sub-task still finds its parent.
			localID[key] = reuse
			sum.TasksPresent++
			cmds = append(cmds, repairCompleted(it, reuse, key, known.taskRowBySource[source], sum)...)
			continue
		}
		newID := id.New("t")
		localID[key] = newID

		title := strings.TrimSpace(it.Content)
		if title == "" {
			title = "(no title)"
		}
		args := store.TaskArgs{
			ID:        newID,
			Title:     strptr(title),
			OrderKey:  strptr(orderKey(it.ChildOrder)),
			Priority:  intptr(MapPriority(it.Priority)),
			SourceRef: strptr(source),
			Labels:    it.Labels,
		}

		project := store.InboxID
		if mapped, ok := projectOf[it.ProjectID.String()]; ok {
			project = mapped
		} else if it.ProjectID != "" {
			// The project is archived or missing, so the task goes to the inbox
			// instead of being dropped.
			sum.Reparented++
		}
		args.ProjectID = strptr(project)

		if it.ParentID != "" {
			if parent, ok := localID[it.ParentID.String()]; ok {
				args.ParentID = strptr(parent)
				sum.SubTasks++
			} else {
				sum.Reparented++
			}
		}

		date, clock := splitDue(it.Due)
		if date != "" {
			args.DueDate = strptr(date)
		}
		if clock != "" {
			args.DueTime = strptr(clock)
			// The zone matters once a reminder fires, so keep the name that the
			// account carried instead of losing it in the conversion.
			if it.Due != nil && it.Due.Timezone != "" {
				args.DueTz = strptr(it.Due.Timezone)
			}
		}
		if it.Deadline != nil && it.Deadline.Date != "" {
			args.Deadline = strptr(it.Deadline.Date)
		}
		if min := it.Duration.Minutes(); min > 0 {
			args.DurationMin = intptr(min)
		}

		var repeat string
		var rec Recurrence
		if it.Due != nil && bool(it.Due.IsRecurring) && strings.TrimSpace(it.Due.String) != "" {
			if converted, ok := ConvertRecurrence(it.Due.String); ok {
				rec = converted
				sum.Recurring++
			} else {
				repeat = strings.TrimSpace(it.Due.String)
				sum.RecurrenceFailed++
				if len(sum.RecurrenceKept) < 10 {
					sum.RecurrenceKept = append(sum.RecurrenceKept, repeat)
				}
			}
		}

		section := ""
		if it.SectionID != "" {
			if name, ok := sections[it.SectionID.String()]; ok && name != "" {
				section = name
				sum.SectionsFolded++
			}
		}
		notes := comments[key]
		if len(notes) > 0 {
			sum.CommentsFolded += len(notes)
		}
		desc := foldDescription(it.Description, section, repeat, notes)
		if desc != "" {
			args.Description = strptr(desc)
		}

		completed := bool(it.Checked)
		// task_complete moves a repeating task to its next date instead of
		// closing it, so a completed repeating task takes the rule after the
		// completion, in a second command.
		if rec.RRule != "" && !completed {
			args.RRule = strptr(rec.RRule)
			if rec.FromCompletion {
				args.FromComplete = boolptr(true)
			}
		}
		cmds = append(cmds, command("task_add", "import-task-"+key, args))
		sum.Tasks++

		if completed {
			cmds = append(cmds, command("task_complete", "import-done-"+key, store.IDArgs{ID: newID}))
			sum.Completed++
			if rec.RRule != "" {
				late := store.TaskArgs{ID: newID, RRule: strptr(rec.RRule)}
				if rec.FromCompletion {
					late.FromComplete = boolptr(true)
				}
				cmds = append(cmds, command("task_update", "import-rrule-"+key, late))
			}
		}
	}
	return cmds, nil
}

// repairCompleted finishes a task that an earlier run left half written.
//
// A completed task costs two or three commands: task_add, then task_complete,
// then task_update for the rule of a repeating task. A run that died between
// them left the task open for ever, because a second run finds the row by its
// source_ref and skips everything that followed it. The completed state and the
// repeat rule of a completed repeating task were both lost in silence, and
// silence is the part that breaks the zero loss promise.
//
// The uuids are the same as the first run used, so a command that did apply is
// a no-op through applied_command. A run over an account that is already whole
// builds nothing here, so a plain second import still writes zero commands.
func repairCompleted(it Item, localID, key string, held store.Task, sum *Summary) []store.Command {
	if !bool(it.Checked) {
		return nil
	}
	var cmds []store.Command
	if held.State == "open" {
		cmds = append(cmds, command("task_complete", "import-done-"+key, store.IDArgs{ID: localID}))
	}
	if it.Due != nil && bool(it.Due.IsRecurring) && strings.TrimSpace(it.Due.String) != "" {
		if rec, ok := ConvertRecurrence(it.Due.String); ok && (held.RRule == nil || *held.RRule == "") {
			args := store.TaskArgs{ID: localID, RRule: strptr(rec.RRule)}
			if rec.FromCompletion {
				args.FromComplete = boolptr(true)
			}
			cmds = append(cmds, command("task_update", "import-rrule-"+key, args))
		}
	}
	if len(cmds) > 0 {
		sum.Resumed++
	}
	return cmds
}

// MapPriority turns a Todoist API priority into ours.
//
// Todoist sends 4 for the p1 that a user sees and 1 for p4, so the two scales
// run in opposite directions. Our store keeps the number that the user sees,
// where 1 is the highest and 4 is none.
func MapPriority(api int) int {
	switch {
	case api >= 4:
		return 1
	case api <= 1:
		return 4
	default:
		return 5 - api
	}
}

// splitDue returns the date as YYYY-MM-DD and the time as HH:MM. A Todoist due
// date is a plain date, a floating timestamp, or a UTC timestamp with a
// separate timezone name.
func splitDue(d *Due) (string, string) {
	if d == nil || strings.TrimSpace(d.Date) == "" {
		return "", ""
	}
	text := strings.TrimSpace(d.Date)
	if !strings.Contains(text, "T") {
		return text, ""
	}
	if strings.HasSuffix(text, "Z") || strings.Contains(text[10:], "+") {
		when, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return text[:10], ""
		}
		// The user reads the time in the timezone of the account, so a fixed
		// timestamp moves into that zone. A zone that this system does not know
		// keeps the UTC time.
		if d.Timezone != "" {
			if loc, err := time.LoadLocation(d.Timezone); err == nil {
				when = when.In(loc)
			}
		}
		return when.Format("2006-01-02"), when.Format("15:04")
	}
	when, err := time.Parse("2006-01-02T15:04:05", text)
	if err != nil {
		if when, err2 := time.Parse("2006-01-02T15:04", text); err2 == nil {
			return when.Format("2006-01-02"), when.Format("15:04")
		}
		return text[:10], ""
	}
	return when.Format("2006-01-02"), when.Format("15:04")
}

// foldDescription puts the parts that our schema cannot hold into the task
// description: the section name, a repeat rule that did not convert, and the
// comments.
func foldDescription(base, section, repeat string, notes []Note) string {
	var b strings.Builder
	if section != "" {
		b.WriteString("Section: " + section + "\n")
	}
	base = strings.TrimRight(base, "\n")
	if base != "" {
		b.WriteString(base + "\n")
	}
	if repeat != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Repeat: " + repeat + "\n")
	}
	if len(notes) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Comments:\n")
		for _, n := range notes {
			b.WriteString("- " + strings.TrimSpace(n.Content) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Write prints the summary for a person.
func (s Summary) Write(w io.Writer) {
	if s.DryRun {
		fmt.Fprintln(w, "Dry run. Nothing was written.")
	}
	fmt.Fprintf(w, "Projects: %d new, %d already in the database", s.Projects, s.ProjectsPresent)
	if s.ProjectsArchived > 0 {
		fmt.Fprintf(w, ", %d archived and skipped", s.ProjectsArchived)
	}
	fmt.Fprintln(w, ".")
	fmt.Fprintf(w, "Labels: %d new, %d already in the database.\n", s.Labels, s.LabelsPresent)
	fmt.Fprintf(w, "Tasks: %d new, %d already in the database.\n", s.Tasks, s.TasksPresent)
	fmt.Fprintf(w, "Of the new tasks: sub-tasks %d, complete %d, repeating %d.\n", s.SubTasks, s.Completed, s.Recurring)
	if s.RecurrenceFailed > 0 {
		fmt.Fprintf(w, "Repeat rules that did not convert: %d. The original words are in the description.\n", s.RecurrenceFailed)
		for _, text := range s.RecurrenceKept {
			fmt.Fprintf(w, "  %s\n", text)
		}
	} else {
		fmt.Fprintln(w, "Repeat rules that did not convert: 0.")
	}
	fmt.Fprintf(w, "Section names moved into a description: %d.\n", s.SectionsFolded)
	fmt.Fprintf(w, "Comments moved into a description: %d.\n", s.CommentsFolded)
	if s.ProjectComments > 0 {
		fmt.Fprintf(w, "Project comments have no place in our model, so %d were skipped.\n", s.ProjectComments)
	}
	if s.FiltersSkipped > 0 {
		fmt.Fprintf(w, "Saved filters have no table yet, so %d were not written. "+
			"Every query is here, and the grammar is the same:\n", s.FiltersSkipped)
		for _, f := range s.FiltersKept {
			fmt.Fprintf(w, "  %s\n", f)
		}
	}
	if s.Resumed > 0 {
		fmt.Fprintf(w, "Rows repaired after an interrupted run: %d.\n", s.Resumed)
	}
	if s.Reparented > 0 {
		fmt.Fprintf(w, "Rows with a missing parent: %d. They went to the top level or to the inbox.\n", s.Reparented)
	}
	if s.ArchivedTasks > 0 {
		fmt.Fprintf(w, "Todoist reports %d completed tasks in the archive. A full sync does not send them, so they are not here.\n", s.ArchivedTasks)
	}
	fmt.Fprintf(w, "Commands: %d, failed: %d.\n", s.Commands, s.Failed)
	for _, e := range s.Errors {
		fmt.Fprintf(w, "  error: %s\n", e)
	}
	if s.Requests > 0 {
		fmt.Fprintf(w, "HTTP requests to Todoist: %d.\n", s.Requests)
	}
	if !s.DryRun {
		fmt.Fprintf(w, "Database version: %d.\n", s.Version)
	}
	fmt.Fprintf(w, "Elapsed time: %s.\n", s.Elapsed.Round(time.Millisecond))
}

func command(kind, uuid string, args any) store.Command {
	raw, err := json.Marshal(args)
	if err != nil { // a struct of strings and numbers cannot fail here
		panic(err)
	}
	return store.Command{UUID: uuid, Type: kind, Args: raw}
}

// orderKey keeps the Todoist order. The column is text, so the number needs a
// fixed width to sort correctly.
func orderKey(n int) string {
	if n < 0 {
		n = 0
	}
	if n > 999999 {
		n = 999999
	}
	return fmt.Sprintf("m%06d", n)
}

func strptr(s string) *string { return &s }
func intptr(n int) *int       { return &n }
func boolptr(b bool) *bool    { return &b }
