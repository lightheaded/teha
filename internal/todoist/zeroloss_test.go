// SPDX-License-Identifier: AGPL-3.0-or-later

package todoist

// The zero loss test for milestone M1. The exit test in docs/PLAN.md §8 reads
// "Todoist account imports with zero loss", and §4 lists what the import must
// carry: sub-projects, sections, labels, comments, filters and completed
// history, recurrence through the parser, and Todoist ids in source_ref.
//
// Every fixture here is synthetic. No real id, no real project name, no real
// personal content and no token reached this tree. The account is invented to
// hold the hard cases, and only the hard cases:
//
//	a project two levels deep         300011 under 300010, 300012 under 300011
//	a section                         400010 Winter, 400020 Documents
//	several labels on one task        600010 carries errand, phone and õues
//	a sub-task three levels deep      600013 under 600012 under 600011
//	a completed task                  600023
//	an "every!" rule from completion  600024, and 600025 which is also complete
//	a deadline and a duration         600020
//	comments                          700010 and 700011 on one task
//	a saved filter                    800010 and 800011
//	an empty title                    600021
//	an emoji and a right to left mark 600022
//	a due date with a time zone       600026
//	a task in a deleted project       600027
//	a task in a project never sent    600028
//	a task in an archived project     600029
//	a sub-task with no parent         600030
//
// The read runs over two pages, so the cursor loop and the request count are
// under test as well.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lightheaded/teha/internal/store"
)

// zeroLossServer serves the two pages and counts the requests. The second page
// arrives only when the client follows the cursor, which is what the real API
// asks of a reader.
type zeroLossServer struct {
	*httptest.Server
	requests atomic.Int64
	fullSync atomic.Int64
	bodies   []string
}

func newZeroLossServer(t *testing.T) *zeroLossServer {
	t.Helper()
	pages := map[string][]byte{}
	for name, file := range map[string]string{"": "page1.json", "zeroloss-page-2": "page2.json"} {
		body, err := os.ReadFile(filepath.Join("testdata", "zeroloss", file))
		if err != nil {
			t.Fatalf("the fixture %s did not load: %v", file, err)
		}
		pages[name] = body
	}
	z := &zeroLossServer{}
	z.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("the form did not parse: %v", err)
		}
		z.requests.Add(1)
		if r.PostForm.Get("sync_token") == "*" && r.PostForm.Get("cursor") == "" {
			z.fullSync.Add(1)
		}
		if got := r.PostForm.Get("resource_types"); !strings.Contains(got, "filters") {
			t.Errorf("resource_types = %q, and it must ask for the saved filters", got)
		}
		body, ok := pages[r.PostForm.Get("cursor")]
		if !ok {
			t.Errorf("the client asked for the unknown cursor %q", r.PostForm.Get("cursor"))
			http.Error(w, "no such cursor", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(z.Server.Close)
	return z
}

func readZeroLoss(t *testing.T, z *zeroLossServer) (*Sync, *Client) {
	t.Helper()
	c := New("test-token")
	c.Endpoint = z.Server.URL
	c.MinInterval = 0
	c.Sleep = func(time.Duration) {}
	data, err := c.Sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("the read failed: %v", err)
	}
	return data, c
}

func openZeroLossStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "teha.db"))
	if err != nil {
		t.Fatalf("the store did not open: %v", err)
	}
	st.Now = func() time.Time { return time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { st.Close() })
	return st
}

// TestZeroLossEveryInputRowArrives is the exit test itself. It walks the
// fixture, not a list of expected numbers, so a row added to the fixture is a
// row the test demands.
func TestZeroLossEveryInputRowArrives(t *testing.T) {
	z := newZeroLossServer(t)
	data, _ := readZeroLoss(t, z)
	st := openZeroLossStore(t)

	sum, err := Import(st, data, Options{})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if sum.Failed != 0 {
		t.Fatalf("%d commands failed: %v", sum.Failed, sum.Errors)
	}

	// --- every task, and every id in source_ref -----------------------------
	tasks := bySource(t, st)
	var wantTasks, deletedTasks []string
	for _, it := range data.Items {
		if bool(it.IsDeleted) {
			deletedTasks = append(deletedTasks, it.ID.String())
			continue
		}
		wantTasks = append(wantTasks, it.ID.String())
	}
	if len(wantTasks) == 0 {
		t.Fatal("the fixture holds no live task, so this test proves nothing")
	}
	for _, id := range wantTasks {
		task, ok := tasks[id]
		if !ok {
			t.Errorf("the task %s is in the payload and not in the store", id)
			continue
		}
		if task.SourceRef == nil || *task.SourceRef != SourcePrefix+id {
			t.Errorf("the task %s carries source_ref %v, want %q", id, task.SourceRef, SourcePrefix+id)
		}
	}
	for _, id := range deletedTasks {
		if _, ok := tasks[id]; ok {
			t.Errorf("the deleted task %s arrived", id)
		}
	}
	if len(tasks) != len(wantTasks) {
		t.Errorf("the store holds %d tasks for %d live rows in the payload", len(tasks), len(wantTasks))
	}

	// --- every project and every label --------------------------------------
	// A project and a label have no source_ref column, so the name is the only
	// key. docs/BACKLOG.md records that gap.
	projects, err := st.Projects()
	if err != nil {
		t.Fatal(err)
	}
	projectByName := map[string]store.Project{}
	for _, p := range projects {
		projectByName[p.Name] = p
	}
	for _, p := range data.Projects {
		name := strings.TrimSpace(p.Name)
		switch {
		case p.IsInbox():
			continue // the Todoist inbox maps onto ours
		case bool(p.IsDeleted):
			if _, ok := projectByName[name]; ok {
				t.Errorf("the deleted project %q arrived", name)
			}
		case bool(p.IsArchived):
			// A full sync sends no live task for an archived project, so the
			// importer counts it and moves on. The summary has to say so.
			if _, ok := projectByName[name]; ok {
				t.Errorf("the archived project %q arrived, and the summary says it was skipped", name)
			}
		default:
			if _, ok := projectByName[name]; !ok {
				t.Errorf("the project %q is in the payload and not in the store", name)
			}
		}
	}

	labels, err := st.Labels()
	if err != nil {
		t.Fatal(err)
	}
	labelByName := map[string]store.Label{}
	for _, l := range labels {
		labelByName[l.Name] = l
	}
	for _, l := range data.Labels {
		name := strings.TrimSpace(l.Name)
		if bool(l.IsDeleted) {
			if _, ok := labelByName[name]; ok {
				t.Errorf("the deleted label %q arrived", name)
			}
			continue
		}
		if _, ok := labelByName[name]; !ok {
			t.Errorf("the label %q is in the payload and not in the store", name)
		}
	}

	// --- the hard cases, one by one -----------------------------------------

	// A project two levels deep keeps its chain.
	garage, shelves := projectByName["Garage"], projectByName["Garage shelves"]
	household := projectByName["Household"]
	if garage.ParentID == nil || *garage.ParentID != household.ID {
		t.Errorf("the parent of Garage is %v, want Household", garage.ParentID)
	}
	if shelves.ParentID == nil || *shelves.ParentID != garage.ID {
		t.Errorf("the parent of Garage shelves is %v, want Garage", shelves.ParentID)
	}

	// Several labels on one task, in one command.
	tyres := tasks["600010"]
	if got := strings.Join(tyres.Labels, ","); got != "errand,phone,õues" {
		t.Errorf("the labels of 600010 are %q, want errand,phone,õues", got)
	}
	// The section is a row of its own, in the project that holds the task.
	// Folding the name into the description stopped when the section table
	// arrived, so the description must NOT carry it.
	sections, err := st.Sections()
	if err != nil {
		t.Fatalf("read the sections: %v", err)
	}
	var winter *store.Section
	for i := range sections {
		if sections[i].Name == "Winter" {
			winter = &sections[i]
		}
	}
	if winter == nil {
		t.Fatal("the section Winter is in the payload and not in the store")
	}
	if winter.ProjectID != tyres.ProjectID {
		t.Errorf("the section Winter sits in project %q, want %q", winter.ProjectID, tyres.ProjectID)
	}
	if tyres.SectionID == nil || *tyres.SectionID != winter.ID {
		t.Errorf("the section of 600010 is %v, want the id of Winter", tyres.SectionID)
	}
	if strings.Contains(tyres.Description, "Section: Winter") {
		t.Errorf("the section name still folds into the description:\n%s", tyres.Description)
	}

	// The description of the task survives, and both comments arrive as rows
	// of their own, oldest comment first.
	if !strings.Contains(tyres.Description, "Four of them, and one spare.") {
		t.Errorf("the description of 600010 is lost:\n%s", tyres.Description)
	}
	talk, err := st.CommentsFor(tyres.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	bodies := make([]string, 0, len(talk))
	for _, c := range talk {
		bodies = append(bodies, c.Body)
	}
	if len(bodies) != 2 {
		t.Fatalf("600010 carries %d comments, want 2: %v", len(bodies), bodies)
	}
	if !strings.Contains(bodies[0], "Book the garage") || !strings.Contains(bodies[1], "The bolts are") {
		t.Errorf("the comments are out of order or lost: %v", bodies)
	}
	for _, c := range bodies {
		if strings.Contains(c, "A comment that was deleted") {
			t.Error("a deleted comment arrived as a row")
		}
	}

	// A sub-task three levels deep points at the right parent at every level.
	chain := [][2]string{{"600011", "600010"}, {"600012", "600011"}, {"600013", "600012"}}
	for _, pair := range chain {
		kid, parent := tasks[pair[0]], tasks[pair[1]]
		if kid.ParentID == nil || *kid.ParentID != parent.ID {
			t.Errorf("the parent of %s is %v, want %s", pair[0], kid.ParentID, pair[1])
		}
	}

	// A deadline and a duration.
	passport := tasks["600020"]
	if d := passport.Deadline; d == nil || *d != "2026-10-01" {
		t.Errorf("the deadline of 600020 is %v", d)
	}
	if d := passport.DurationMin; d == nil || *d != 45 {
		t.Errorf("the duration of 600020 is %v", d)
	}
	if d := passport.DueTime; d == nil || *d != "09:30" {
		t.Errorf("the due time of 600020 is %v", d)
	}

	// A completed task is closed and keeps its completion time.
	alarm := tasks["600023"]
	if alarm.State != "done" {
		t.Errorf("the state of 600023 is %q, want done", alarm.State)
	}
	if alarm.CompletedAt == nil {
		t.Error("600023 has no completion time")
	}

	// An "every!" rule counts from completion.
	drain := tasks["600024"]
	if r := drain.RRule; r == nil || *r != "FREQ=WEEKLY;INTERVAL=3" {
		t.Errorf("the rule of 600024 is %v, want FREQ=WEEKLY;INTERVAL=3", r)
	}
	if !drain.FromComplete {
		t.Error("the \"every!\" form of 600024 did not set rrule_from_completion")
	}
	// A task that is complete and repeating keeps both: task_complete would
	// move a repeating task to its next date, so the rule arrives after it.
	herbs := tasks["600025"]
	if herbs.State != "done" {
		t.Errorf("the state of 600025 is %q, want done", herbs.State)
	}
	if r := herbs.RRule; r == nil || *r != "FREQ=DAILY;INTERVAL=2" {
		t.Errorf("the rule of 600025 is %v, want FREQ=DAILY;INTERVAL=2", r)
	}
	if !herbs.FromComplete {
		t.Error("600025 lost its from-completion flag")
	}

	// An empty title still arrives, because a row that vanishes is a loss.
	empty := tasks["600021"]
	if empty.Title != "(no title)" {
		t.Errorf("the title of the empty task is %q, want (no title)", empty.Title)
	}
	if !strings.Contains(empty.Description, "A row with no title at all.") {
		t.Errorf("the empty task lost its description: %q", empty.Description)
	}

	// An emoji and a right to left script survive byte for byte.
	if got, want := tasks["600022"].Title, "🚗 rehvivahetus שירות"; got != want {
		t.Errorf("the title of 600022 is %q, want %q", got, want)
	}

	// A due date with a time zone reads in that zone.
	ferry := tasks["600026"]
	wantDate, wantClock := "2026-09-10", "05:30"
	if _, err := time.LoadLocation("Pacific/Auckland"); err == nil {
		wantDate, wantClock = "2026-09-10", "17:30"
	}
	if d := ferry.DueDate; d == nil || *d != wantDate {
		t.Errorf("the due date of 600026 is %v, want %s", d, wantDate)
	}
	if d := ferry.DueTime; d == nil || *d != wantClock {
		t.Errorf("the due time of 600026 is %v, want %s", d, wantClock)
	}
	if d := ferry.DueTz; d == nil || *d != "Pacific/Auckland" {
		t.Errorf("the zone of 600026 is %v, want Pacific/Auckland", d)
	}

	// A task whose project is gone, missing or archived lands in the inbox and
	// is never dropped. Both clients agreed on this rule after the bug in
	// docs/POC.md, and the importer is the third one.
	for _, id := range []string{"600027", "600028", "600029"} {
		if got := tasks[id].ProjectID; got != store.InboxID {
			t.Errorf("the task %s is in project %q, want the inbox", id, got)
		}
	}
	// A sub-task whose parent never arrived becomes a top level task.
	if p := tasks["600030"].ParentID; p != nil {
		t.Errorf("the parent of 600030 is %v, want none", p)
	}

	// --- the saved filters --------------------------------------------------
	// Our schema has no filter table, so the importer writes none. It must say
	// so and it must print every query, or the exit is not a zero loss exit.
	if sum.FiltersSkipped != 2 {
		t.Errorf("filters skipped = %d, want 2", sum.FiltersSkipped)
	}
	var report strings.Builder
	sum.Write(&report)
	for _, want := range []string{"Two weeks: 14 days & !%errand", "Calls: %phone & !subtask"} {
		if !strings.Contains(report.String(), want) {
			t.Errorf("the summary does not carry the saved filter %q:\n%s", want, report.String())
		}
	}
	if strings.Contains(report.String(), "A filter that was deleted") {
		t.Error("a deleted filter reached the summary")
	}
	// The queries it kept must compile in our own grammar, or a person cannot
	// type them back in.
	if !strings.Contains(report.String(), "Saved filters have no table yet") {
		t.Errorf("the summary does not explain the gap:\n%s", report.String())
	}

	// --- the archive count --------------------------------------------------
	if sum.ArchivedTasks != 15 {
		t.Errorf("the archive count = %d, want 15", sum.ArchivedTasks)
	}
	if !strings.Contains(report.String(), "archive") {
		t.Error("the summary does not mention the archive that a full sync never sends")
	}
	if !strings.Contains(report.String(), "archived and skipped") {
		t.Error("the summary does not mention the archived project")
	}
}

// TestZeroLossASecondImportWritesNothing is the other half of zero loss: a
// person who runs the import twice must not end with two of everything.
func TestZeroLossASecondImportWritesNothing(t *testing.T) {
	z := newZeroLossServer(t)
	st := openZeroLossStore(t)

	data, _ := readZeroLoss(t, z)
	if _, err := Import(st, data, Options{}); err != nil {
		t.Fatalf("the first import failed: %v", err)
	}
	before := stateOf(t, st)
	firstVersion, err := st.Version()
	if err != nil {
		t.Fatal(err)
	}

	again, _ := readZeroLoss(t, z)
	sum, err := Import(st, again, Options{})
	if err != nil {
		t.Fatalf("the second import failed: %v", err)
	}
	if sum.Commands != 0 {
		t.Errorf("the second run built %d commands, want none", sum.Commands)
	}
	if sum.Tasks != 0 || sum.Projects != 0 || sum.Labels != 0 {
		t.Errorf("the second run wrote %d tasks, %d projects and %d labels",
			sum.Tasks, sum.Projects, sum.Labels)
	}
	if got := stateOf(t, st); got != before {
		t.Errorf("the second run changed the account:\n%s", firstLineThatDiffers(before, got))
	}
	if got, _ := st.Version(); got != firstVersion {
		t.Errorf("the second run moved the version from %d to %d", firstVersion, got)
	}
}

// TestZeroLossResumesAfterAnInterruption is the promise in §4 of the plan: the
// importer "resumes after an interruption instead of starting again".
//
// The failure is simulated at every point in the command stream, which is the
// only honest way to test it: an interruption between task_add and the command
// that closes a completed task is a different case from an interruption
// between two task_add calls.
func TestZeroLossResumesAfterAnInterruption(t *testing.T) {
	z := newZeroLossServer(t)

	// The state a clean run reaches, which every interrupted run must reach.
	clean := openZeroLossStore(t)
	data, _ := readZeroLoss(t, z)
	if _, err := Import(clean, data, Options{}); err != nil {
		t.Fatal(err)
	}
	want := stateOf(t, clean)

	// How many commands a clean run builds, so the walk covers every cut.
	var count int
	{
		st := openZeroLossStore(t)
		known, err := readExisting(st)
		if err != nil {
			t.Fatal(err)
		}
		var sum Summary
		cmds, err := buildCommands(data, known, &sum)
		if err != nil {
			t.Fatal(err)
		}
		count = len(cmds)
	}
	if count < 10 {
		t.Fatalf("the fixture builds %d commands, which is too few to cut", count)
	}

	for cut := 0; cut <= count; cut++ {
		st := openZeroLossStore(t)
		known, err := readExisting(st)
		if err != nil {
			t.Fatal(err)
		}
		var sum Summary
		cmds, err := buildCommands(data, known, &sum)
		if err != nil {
			t.Fatal(err)
		}
		// The run dies after `cut` commands, which is what a lost connection or
		// a killed process looks like from the database side.
		if cut > 0 {
			if _, res, err := st.Apply(cmds[:cut]); err != nil {
				t.Fatalf("cut %d: the partial run failed: %v", cut, err)
			} else {
				for _, r := range res {
					if !r.OK {
						t.Fatalf("cut %d: the command %s failed: %s", cut, r.UUID, r.Error)
					}
				}
			}
		}

		// The person runs the import again.
		resumed, err := Import(st, data, Options{})
		if err != nil {
			t.Fatalf("cut %d: the second run failed: %v", cut, err)
		}
		if resumed.Failed != 0 {
			t.Fatalf("cut %d: %d commands failed on the resume: %v", cut, resumed.Failed, resumed.Errors)
		}
		if got := stateOf(t, st); got != want {
			t.Fatalf("cut %d of %d: a run that died and resumed did not reach the state of a clean run.\n%s",
				cut, count, firstLineThatDiffers(want, got))
		}
	}
}

// TestZeroLossStaysInsideTheDocumentedLimits locks the pacing that §12 of the
// plan records: 1 000 partial-sync and 100 full-sync requests per 15 minutes,
// 100 commands per request, and a body under 1 MiB.
func TestZeroLossStaysInsideTheDocumentedLimits(t *testing.T) {
	z := newZeroLossServer(t)
	data, client := readZeroLoss(t, z)

	// The read. Two pages, so two requests, and exactly one of them is a full
	// sync. A cursor page is a paged read of the same full sync, and the limit
	// that binds is the partial one.
	if got := z.requests.Load(); got != 2 {
		t.Errorf("the read cost %d requests, want 2 for two pages", got)
	}
	if got := z.fullSync.Load(); got > 100 {
		t.Errorf("the read sent %d full syncs, and the documented limit is 100 per 15 minutes", got)
	}
	if got := z.fullSync.Load(); got != 1 {
		t.Errorf("the read sent %d full syncs, want exactly 1", got)
	}
	if got := int64(client.Requests); got != z.requests.Load() {
		t.Errorf("the client counted %d requests and the server saw %d", got, z.requests.Load())
	}
	if got := z.requests.Load(); got > 1000 {
		t.Errorf("the read sent %d requests, and the documented limit is 1 000 per 15 minutes", got)
	}
	// The client paces itself, so a big account cannot reach the limit by
	// accident either.
	if New("x").MinInterval < time.Second {
		t.Error("the default pace is faster than one request per second")
	}

	// The write. Every batch holds at most 100 commands and fits in 1 MiB.
	st := openZeroLossStore(t)
	known, err := readExisting(st)
	if err != nil {
		t.Fatal(err)
	}
	var sum Summary
	cmds, err := buildCommands(data, known, &sum)
	if err != nil {
		t.Fatal(err)
	}
	if DefaultBatch > 100 {
		t.Errorf("the default batch is %d commands, and the documented limit is 100", DefaultBatch)
	}
	const oneMiB = 1 << 20
	for from := 0; from < len(cmds); from += DefaultBatch {
		to := from + DefaultBatch
		if to > len(cmds) {
			to = len(cmds)
		}
		batch := cmds[from:to]
		if len(batch) > 100 {
			t.Errorf("a batch holds %d commands, want at most 100", len(batch))
		}
		body, err := json.Marshal(map[string]any{"since": 0, "commands": batch})
		if err != nil {
			t.Fatal(err)
		}
		if len(body) >= oneMiB {
			t.Errorf("a batch of %d commands is %d bytes, and the documented limit is 1 MiB",
				len(batch), len(body))
		}
	}
}

// TestZeroLossHandlesTheUnknownProjectTheSameWayEverywhere locks bug 6 of
// docs/POC.md. The command line client created a project for a name it did not
// know and the web app used the inbox, so two clients disagreed. The rule that
// came out of it is that a typo must never make a junk project.
//
// Three write paths meet that rule in three ways, and all three are correct:
// the importer and the command line client fall back to the inbox and say so,
// and the store refuses to guess, so an agent gets an error it can repair. What
// none of them does is invent a project.
func TestZeroLossHandlesTheUnknownProjectTheSameWayEverywhere(t *testing.T) {
	z := newZeroLossServer(t)
	data, _ := readZeroLoss(t, z)
	st := openZeroLossStore(t)
	if _, err := Import(st, data, Options{}); err != nil {
		t.Fatal(err)
	}

	before, err := st.Projects()
	if err != nil {
		t.Fatal(err)
	}

	// The importer: three tasks name a project that is deleted, missing or
	// archived, and all three are in the inbox.
	tasks := bySource(t, st)
	for _, id := range []string{"600027", "600028", "600029"} {
		if got := tasks[id].ProjectID; got != store.InboxID {
			t.Errorf("the importer put %s in %q, want the inbox", id, got)
		}
	}
	// The store: a command that names a project by a name nobody holds fails,
	// and it creates nothing. The failure text names the project, so an agent
	// can repair itself instead of failing a session.
	args := store.TaskArgs{ID: "t_unknown", Title: strptr("Order gravel"), Project: strptr("Garden")}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	_, res, err := st.Apply([]store.Command{{UUID: "unknown-project", Type: "task_add", Args: raw}})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].OK {
		t.Error("the store accepted a project name that nobody holds")
	}
	if !strings.Contains(res[0].Error, "Garden") {
		t.Errorf("the failure text is %q, and it must name the project", res[0].Error)
	}

	after, err := st.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("projects went from %d to %d, so a typo made a junk project", len(before), len(after))
	}
	for _, p := range after {
		if strings.EqualFold(p.Name, "Garden") {
			t.Error("a project named Garden exists, and nobody asked for one")
		}
	}

	// A prefix that matches two projects changes nothing either. "Garage" and
	// "Garage shelves" both start with Gara.
	args = store.TaskArgs{ID: "t_ambiguous", Title: strptr("Sweep the floor"), Project: strptr("Gara")}
	raw, err = json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	_, res, err = st.Apply([]store.Command{{UUID: "ambiguous-project", Type: "task_add", Args: raw}})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].OK {
		t.Error("an ambiguous prefix was accepted, so the task went somewhere at random")
	}
	if !strings.Contains(res[0].Error, "Garage") {
		t.Errorf("the failure text is %q, and it must list what the prefix matched", res[0].Error)
	}
}

// TestZeroLossFixtureCarriesNoRealData is a guard on the fixture tree itself.
// A recorded payload from a real account is the one thing that must never
// reach this repository.
func TestZeroLossFixtureCarriesNoRealData(t *testing.T) {
	for _, name := range []string{"page1.json", "page2.json"} {
		body, err := os.ReadFile(filepath.Join("testdata", "zeroloss", name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		// A real Todoist id is a long number or a 22 character string, and a
		// real payload carries a user id, an email address and a token.
		for _, forbidden := range []string{"@", "user_id", "email", "access_token",
			"api_token", "todoist.com", "Bearer "} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s carries %q, which a synthetic fixture never needs", name, forbidden)
			}
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
	}
}

// --- helpers ----------------------------------------------------------------

// stateOf renders the whole account as text, so two runs compare line by line.
// The version and the timestamps stay out: they count the writes, and an
// interrupted run writes the same rows in more calls.
func stateOf(t *testing.T, st *store.Store) string {
	t.Helper()
	delta, err := st.Pull(0)
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	var lines []string
	for _, p := range delta.Projects {
		lines = append(lines, fmt.Sprintf("project name=%q color=%s parent=%v order=%s inbox=%v gone=%v",
			p.Name, p.Color, p.ParentID != nil, p.OrderKey, p.IsInbox, p.DeletedAt != nil))
	}
	for _, l := range delta.Labels {
		lines = append(lines, fmt.Sprintf("label name=%q color=%s gone=%v", l.Name, l.Color, l.DeletedAt != nil))
	}
	for _, task := range delta.Tasks {
		lines = append(lines, fmt.Sprintf("task src=%v title=%q desc=%q p=%d due=%v time=%v tz=%v "+
			"rrule=%v fromdone=%v deadline=%v dur=%v state=%s done=%v parentset=%v labels=%v order=%s",
			derefText(task.SourceRef), task.Title, task.Description, task.Priority,
			derefText(task.DueDate), derefText(task.DueTime), derefText(task.DueTz),
			derefText(task.RRule), task.FromComplete, derefText(task.Deadline),
			derefNumber(task.DurationMin), task.State, task.CompletedAt != nil, task.ParentID != nil,
			task.Labels, task.OrderKey))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func derefText(p *string) string {
	if p == nil {
		return "-"
	}
	return *p
}

func derefNumber(p *int) string {
	if p == nil {
		return "-"
	}
	return strconv.Itoa(*p)
}

// firstLineThatDiffers shrinks a failure by hand, because two accounts of
// thirty rows each print as sixty lines nobody reads.
func firstLineThatDiffers(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			return fmt.Sprintf("line %d: a clean run has\n  %s\nand this run has\n  %s", i+1, w, g)
		}
	}
	return "the two accounts are equal, so the comparison itself is broken"
}
