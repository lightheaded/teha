// SPDX-License-Identifier: AGPL-3.0-or-later

package todoist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lightheaded/teha/internal/store"
)

// fixtureServer serves the recorded sync payload. It answers every POST the
// same way, which is what a full sync of a small account looks like.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "sync.json"))
	if err != nil {
		t.Fatalf("the fixture did not load: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("authorization header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readFixture(t *testing.T, endpoint string) *Sync {
	t.Helper()
	c := New("test-token")
	c.Endpoint = endpoint
	c.MinInterval = 0
	c.Sleep = func(time.Duration) {}
	data, err := c.Sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("the read failed: %v", err)
	}
	return data
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "teha.db"))
	if err != nil {
		t.Fatalf("the store did not open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// bySource indexes the tasks of the store by their Todoist id.
func bySource(t *testing.T, st *store.Store) map[string]store.Task {
	t.Helper()
	delta, err := st.Pull(0)
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	out := map[string]store.Task{}
	for _, task := range delta.Tasks {
		if task.SourceRef == nil {
			t.Errorf("task %q has no source_ref", task.Title)
			continue
		}
		out[strings.TrimPrefix(*task.SourceRef, SourcePrefix)] = task
	}
	return out
}

func TestImportEndToEnd(t *testing.T) {
	srv := fixtureServer(t)
	st := openTestStore(t)

	sum, err := Import(st, readFixture(t, srv.URL), Options{})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if sum.Failed != 0 {
		t.Fatalf("%d commands failed: %v", sum.Failed, sum.Errors)
	}

	want := map[string]int{
		"projects":          3,
		"projects archived": 1,
		"labels":            2,
		"tasks":             12,
		"sub-tasks":         1,
		"completed":         1,
		"recurring":         5,
		"recurrence failed": 1,
		"sections":          2,
		"sections skipped":  1,
		"comments folded":   1,
		"project comments":  1,
		"archived tasks":    7,
	}
	got := map[string]int{
		"projects":          sum.Projects,
		"projects archived": sum.ProjectsArchived,
		"labels":            sum.Labels,
		"tasks":             sum.Tasks,
		"sub-tasks":         sum.SubTasks,
		"completed":         sum.Completed,
		"recurring":         sum.Recurring,
		"recurrence failed": sum.RecurrenceFailed,
		"sections":          sum.Sections,
		"sections skipped":  sum.SectionsSkipped,
		"comments folded":   sum.CommentsFolded,
		"project comments":  sum.ProjectComments,
		"archived tasks":    sum.ArchivedTasks,
	}
	for key, n := range want {
		if got[key] != n {
			t.Errorf("summary %s = %d, want %d", key, got[key], n)
		}
	}

	// --- projects -----------------------------------------------------------
	projects, err := st.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 4 {
		t.Fatalf("projects = %d, want 4 with the inbox", len(projects))
	}
	byName := map[string]store.Project{}
	for _, p := range projects {
		byName[p.Name] = p
	}
	for _, name := range []string{"Inbox", "Home", "Work", "Work follow-up"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("the project %q is missing", name)
		}
	}
	if byName["Inbox"].ID != store.InboxID {
		t.Errorf("the Todoist inbox did not map onto %q", store.InboxID)
	}
	if p := byName["Work follow-up"]; p.ParentID == nil || *p.ParentID != byName["Work"].ID {
		t.Errorf("the nested project has the wrong parent: %+v", p.ParentID)
	}
	if byName["Home"].Color != "green" {
		t.Errorf("the color did not carry: %q", byName["Home"].Color)
	}

	// --- labels -------------------------------------------------------------
	labels, err := st.Labels()
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 {
		t.Fatalf("labels = %d, want 2", len(labels))
	}

	// --- tasks --------------------------------------------------------------
	tasks := bySource(t, st)
	if len(tasks) != 12 {
		t.Fatalf("tasks = %d, want 12", len(tasks))
	}
	if _, ok := tasks["5099"]; ok {
		t.Error("a deleted Todoist task must not arrive")
	}

	// A Todoist p1 is 4 on the wire and 1 in our store.
	if p := tasks["5002"].Priority; p != 1 {
		t.Errorf("the priority of the urgent task = %d, want 1", p)
	}
	if p := tasks["5001"].Priority; p != 4 {
		t.Errorf("the priority of the plain task = %d, want 4", p)
	}
	if p := tasks["5006"].Priority; p != 2 {
		t.Errorf("the priority of the p2 task = %d, want 2", p)
	}

	if got := tasks["5002"].Labels; len(got) != 1 || got[0] != "errand" {
		t.Errorf("labels of 5002 = %v, want [errand]", got)
	}
	if d := tasks["5002"].Description; !strings.Contains(d, "The tap in the kitchen drips.") ||
		!strings.Contains(d, "Comments:") || !strings.Contains(d, "on the fridge") {
		t.Errorf("the comment did not fold into the description: %q", d)
	}

	// A Todoist section is a section row now, and the name is out of the
	// description. Two tasks share the section of the Home project, and one task
	// of another project has its own section.
	secs, err := st.Sections()
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 2 {
		t.Fatalf("sections = %d, want 2: %+v", len(secs), secs)
	}
	bySection := map[string]store.Section{}
	for _, sec := range secs {
		bySection[sec.Name] = sec
	}
	errands, ok := bySection["Errands"]
	if !ok {
		t.Fatalf("the section Errands is missing: %+v", secs)
	}
	if errands.ProjectID != byName["Home"].ID {
		t.Errorf("Errands is in project %q, want Home", errands.ProjectID)
	}
	next, ok := bySection["Next actions"]
	if !ok {
		t.Fatalf("the section Next actions is missing: %+v", secs)
	}
	if next.ProjectID != byName["Work"].ID {
		t.Errorf("Next actions is in project %q, want Work", next.ProjectID)
	}
	for _, source := range []string{"5004", "5005"} {
		if got := tasks[source].SectionID; got == nil || *got != errands.ID {
			t.Errorf("section of %s = %v, want the Errands section %q", source, got, errands.ID)
		}
	}
	if got := tasks["5006"].SectionID; got == nil || *got != next.ID {
		t.Errorf("section of 5006 = %v, want the Next actions section %q", got, next.ID)
	}
	if got := tasks["5003"].SectionID; got != nil {
		t.Errorf("section of 5003 = %v, want none", *got)
	}
	for _, source := range []string{"5004", "5005", "5006"} {
		if d := tasks[source].Description; strings.Contains(d, "Section:") {
			t.Errorf("description of %s still folds the section name: %q", source, d)
		}
	}

	if r := tasks["5003"].RRule; r == nil || *r != "FREQ=WEEKLY;BYDAY=MO" {
		t.Errorf("rrule of 5003 = %v", r)
	}
	if d := tasks["5003"].DueDate; d == nil || *d != "2026-08-26" {
		t.Errorf("due date of 5003 = %v", d)
	}
	if r := tasks["5010"].RRule; r == nil || *r != "FREQ=MONTHLY;BYMONTHDAY=-1" {
		t.Errorf("rrule of 5010 = %v", r)
	}
	if tasks["5010"].ProjectID != store.InboxID {
		t.Errorf("5010 is in project %q, want the inbox", tasks["5010"].ProjectID)
	}
	if r := tasks["5011"].RRule; r == nil || *r != "FREQ=MONTHLY;INTERVAL=2" {
		t.Errorf("rrule of 5011 = %v", r)
	}
	if !tasks["5011"].FromComplete {
		t.Error("the \"every!\" form must set rrule_from_completion")
	}

	// A repeat string that does not convert keeps the task, leaves the rule
	// empty and lands in the description.
	if r := tasks["5009"].RRule; r != nil && *r != "" {
		t.Errorf("rrule of 5009 = %v, want none", r)
	}
	if d := tasks["5009"].Description; !strings.Contains(d, "Repeat: every jan 15") {
		t.Errorf("description of 5009 = %q", d)
	}

	// The sub-task points at its parent.
	if p := tasks["5007"].ParentID; p == nil || *p != tasks["5006"].ID {
		t.Errorf("parent of 5007 = %v, want %q", p, tasks["5006"].ID)
	}
	if tasks["5007"].ProjectID != byName["Work"].ID {
		t.Errorf("5007 is in the wrong project")
	}

	// The date, the clock time, the deadline and the duration all carry.
	if d := tasks["5006"].DueDate; d == nil || *d != "2026-08-28" {
		t.Errorf("due date of 5006 = %v", d)
	}
	if d := tasks["5006"].DueTime; d == nil || *d != "09:00" {
		t.Errorf("due time of 5006 = %v", d)
	}
	if d := tasks["5006"].Deadline; d == nil || *d != "2026-08-31" {
		t.Errorf("deadline of 5006 = %v", d)
	}
	if d := tasks["5006"].DurationMin; d == nil || *d != 90 {
		t.Errorf("duration of 5006 = %v", d)
	}

	// A fixed timestamp moves into the timezone of the account, if this system
	// knows the zone.
	wantClock := "13:00"
	if _, err := time.LoadLocation("Europe/Tallinn"); err == nil {
		wantClock = "16:00"
	}
	if d := tasks["5012"].DueTime; d == nil || *d != wantClock {
		t.Errorf("due time of 5012 = %v, want %s", d, wantClock)
	}

	// The completed task is done, and it keeps its rule.
	done := tasks["5008"]
	if done.State != "done" {
		t.Errorf("state of 5008 = %q, want done", done.State)
	}
	if done.CompletedAt == nil {
		t.Error("5008 has no completion time")
	}
	if r := done.RRule; r == nil || *r != "FREQ=WEEKLY;BYDAY=FR" {
		t.Errorf("rrule of 5008 = %v", r)
	}

	// --- the second run must change nothing ---------------------------------
	again, err := Import(st, readFixture(t, srv.URL), Options{})
	if err != nil {
		t.Fatalf("the second import failed: %v", err)
	}
	if again.Failed != 0 {
		t.Fatalf("%d commands failed on the second run: %v", again.Failed, again.Errors)
	}
	if again.Tasks != 0 || again.TasksPresent != 12 {
		t.Errorf("the second run wrote %d tasks and found %d, want 0 and 12", again.Tasks, again.TasksPresent)
	}
	if again.Projects != 0 || again.ProjectsPresent != 3 {
		t.Errorf("the second run wrote %d projects and found %d, want 0 and 3", again.Projects, again.ProjectsPresent)
	}
	if again.Labels != 0 || again.LabelsPresent != 2 {
		t.Errorf("the second run wrote %d labels and found %d, want 0 and 2", again.Labels, again.LabelsPresent)
	}
	if again.Sections != 0 || again.SectionsPresent != 2 {
		t.Errorf("the second run wrote %d sections and found %d, want 0 and 2", again.Sections, again.SectionsPresent)
	}
	if again.Commands != 0 {
		t.Errorf("the second run built %d commands, want none", again.Commands)
	}

	after := bySource(t, st)
	if len(after) != 12 {
		t.Errorf("tasks after the second run = %d, want 12", len(after))
	}
	projectsAfter, err := st.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projectsAfter) != 4 {
		t.Errorf("projects after the second run = %d, want 4", len(projectsAfter))
	}
	labelsAfter, err := st.Labels()
	if err != nil {
		t.Fatal(err)
	}
	if len(labelsAfter) != 2 {
		t.Errorf("labels after the second run = %d, want 2", len(labelsAfter))
	}
	sectionsAfter, err := st.Sections()
	if err != nil {
		t.Fatal(err)
	}
	if len(sectionsAfter) != 2 {
		t.Errorf("sections after the second run = %d, want 2", len(sectionsAfter))
	}
	for _, sec := range sectionsAfter {
		if bySection[sec.Name].ID != sec.ID {
			t.Errorf("the id of the section %q changed, so the second run wrote a copy", sec.Name)
		}
	}
	for source, task := range after {
		if tasks[source].ID != task.ID {
			t.Errorf("the id of %s changed, so the second run wrote a copy", source)
		}
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	srv := fixtureServer(t)
	st := openTestStore(t)

	sum, err := Import(st, readFixture(t, srv.URL), Options{DryRun: true})
	if err != nil {
		t.Fatalf("the dry run failed: %v", err)
	}
	if sum.Tasks != 12 || sum.Projects != 3 || sum.Labels != 2 {
		t.Errorf("the dry run counted %d tasks, %d projects and %d labels", sum.Tasks, sum.Projects, sum.Labels)
	}
	if sum.Commands == 0 {
		t.Error("the dry run must build the commands")
	}
	delta, err := st.Pull(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Tasks) != 0 {
		t.Errorf("the dry run wrote %d tasks", len(delta.Tasks))
	}
	if len(delta.Labels) != 0 {
		t.Errorf("the dry run wrote %d labels", len(delta.Labels))
	}
	if len(delta.Projects) != 1 {
		t.Errorf("projects = %d, want the seeded inbox only", len(delta.Projects))
	}
}

// TestImportBatches proves that a small batch size writes the same rows. The
// real import sends at most 100 commands in one call.
func TestImportBatches(t *testing.T) {
	srv := fixtureServer(t)
	st := openTestStore(t)
	if _, err := Import(st, readFixture(t, srv.URL), Options{Batch: 3}); err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if got := len(bySource(t, st)); got != 12 {
		t.Errorf("tasks = %d, want 12", got)
	}
}

func TestSummaryWriteMentionsTheFacts(t *testing.T) {
	sum := Summary{Tasks: 3, RecurrenceFailed: 1, RecurrenceKept: []string{"every jan 15"}, Elapsed: time.Second}
	var b strings.Builder
	sum.Write(&b)
	for _, want := range []string{"Tasks: 3", "every jan 15", "Elapsed time"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("the summary does not mention %q:\n%s", want, b.String())
		}
	}
}
