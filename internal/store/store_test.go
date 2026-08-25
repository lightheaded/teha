// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { s.Close() })
	return s
}

func cmd(t *testing.T, uuid, kind string, args any) Command {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return Command{UUID: uuid, Type: kind, Args: raw}
}

func ptr[T any](v T) *T { return &v }

func TestInboxExists(t *testing.T) {
	s := newStore(t)
	ps, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].ID != InboxID || !ps[0].IsInbox {
		t.Fatalf("want one inbox project, got %+v", ps)
	}
}

func TestAddAndPull(t *testing.T) {
	s := newStore(t)
	v0, _ := s.Version()
	_, res, err := s.Apply([]Command{
		cmd(t, "u1", "task_add", TaskArgs{ID: "t1", Title: ptr("Buy milk"), DueDate: ptr("2026-08-25"), Labels: []string{"store"}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res[0].OK {
		t.Fatalf("command failed: %s", res[0].Error)
	}
	d, err := s.Pull(v0)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 1 || d.Tasks[0].Title != "Buy milk" {
		t.Fatalf("pull returned %+v", d.Tasks)
	}
	if len(d.Tasks[0].Labels) != 1 || d.Tasks[0].Labels[0] != "store" {
		t.Fatalf("labels missing: %+v", d.Tasks[0].Labels)
	}
	if len(d.Labels) != 1 {
		t.Fatalf("a quick add must create the label row, got %+v", d.Labels)
	}
}

// A retried command must not apply twice. This is what makes the outbox safe
// after a lost response.
func TestCommandIsIdempotent(t *testing.T) {
	s := newStore(t)
	c := cmd(t, "same-uuid", "task_add", TaskArgs{ID: "t1", Title: ptr("Once")})
	if _, _, err := s.Apply([]Command{c}); err != nil {
		t.Fatal(err)
	}
	v1, _ := s.Version()
	if _, res, err := s.Apply([]Command{c}); err != nil {
		t.Fatal(err)
	} else if !res[0].OK {
		t.Fatalf("a replay must report success, got %q", res[0].Error)
	}
	v2, _ := s.Version()
	if v1 != v2 {
		t.Fatalf("a replay changed the version from %d to %d", v1, v2)
	}
	all, err := s.Query("1=1", nil, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("a replay created %d tasks", len(all))
	}
}

func TestPullIsIncremental(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.Apply([]Command{cmd(t, "u1", "task_add", TaskArgs{ID: "t1", Title: ptr("One")})}); err != nil {
		t.Fatal(err)
	}
	mid, _ := s.Version()
	if _, _, err := s.Apply([]Command{cmd(t, "u2", "task_add", TaskArgs{ID: "t2", Title: ptr("Two")})}); err != nil {
		t.Fatal(err)
	}
	d, err := s.Pull(mid)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 1 || d.Tasks[0].ID != "t2" {
		t.Fatalf("an incremental pull returned %+v", d.Tasks)
	}
}

func TestCompleteAndUndo(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.Apply([]Command{cmd(t, "u1", "task_add", TaskArgs{ID: "t1", Title: ptr("Plain")})}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Apply([]Command{cmd(t, "u2", "task_complete", IDArgs{ID: "t1"})}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Task("t1")
	if got.State != "done" || got.CompletedAt == nil {
		t.Fatalf("task is %+v", got)
	}
	if _, _, err := s.Apply([]Command{cmd(t, "u3", "task_uncomplete", IDArgs{ID: "t1"})}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Task("t1")
	if got.State != "open" || got.CompletedAt != nil {
		t.Fatalf("undo left the task as %+v", got)
	}
}

// A recurring task must move to its next date and stay open.
func TestRecurringCompleteAdvances(t *testing.T) {
	cases := []struct {
		name, rrule, due, want string
		fromCompletion         bool
	}{
		{"daily", "FREQ=DAILY", "2026-08-25", "2026-08-26", false},
		{"weekly on Tuesday", "FREQ=WEEKLY;BYDAY=TU", "2026-08-25", "2026-09-01", false},
		{"monthly, two days late", "FREQ=MONTHLY", "2026-08-23", "2026-09-23", false},
		{"overdue by months keeps the slot", "FREQ=WEEKLY;BYDAY=TU", "2026-06-02", "2026-09-01", false},
		{"from completion", "FREQ=DAILY;INTERVAL=3", "2026-06-02", "2026-08-28", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStore(t)
			args := TaskArgs{ID: "t1", Title: ptr("Chore"), DueDate: ptr(c.due), RRule: ptr(c.rrule)}
			if c.fromCompletion {
				args.FromComplete = ptr(true)
			}
			if _, res, err := s.Apply([]Command{cmd(t, "u1", "task_add", args)}); err != nil || !res[0].OK {
				t.Fatalf("add failed: %v %+v", err, res)
			}
			if _, res, err := s.Apply([]Command{cmd(t, "u2", "task_complete", IDArgs{ID: "t1"})}); err != nil || !res[0].OK {
				t.Fatalf("complete failed: %v %+v", err, res)
			}
			got, err := s.Task("t1")
			if err != nil {
				t.Fatal(err)
			}
			if got.State != "open" {
				t.Fatalf("a recurring task closed instead of moving: state %s", got.State)
			}
			if got.DueDate == nil || *got.DueDate != c.want {
				t.Fatalf("next date is %v, want %s", got.DueDate, c.want)
			}
		})
	}
}

func TestUpdateClearsField(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.Apply([]Command{
		cmd(t, "u1", "task_add", TaskArgs{ID: "t1", Title: ptr("Dated"), DueDate: ptr("2026-08-25")}),
		cmd(t, "u2", "task_update", TaskArgs{ID: "t1", Clear: []string{"due_date"}}),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Task("t1")
	if got.DueDate != nil {
		t.Fatalf("due date is still %v", *got.DueDate)
	}
}

func TestUnknownCommandFailsAlone(t *testing.T) {
	s := newStore(t)
	_, res, err := s.Apply([]Command{
		cmd(t, "u1", "task_add", TaskArgs{ID: "t1", Title: ptr("Good")}),
		cmd(t, "u2", "nonsense", IDArgs{ID: "t1"}),
		cmd(t, "u3", "task_add", TaskArgs{ID: "t2", Title: ptr("Also good")}),
	})
	if err != nil {
		t.Fatalf("one bad command must not fail the batch: %v", err)
	}
	if !res[0].OK || res[1].OK || !res[2].OK {
		t.Fatalf("results are %+v", res)
	}
	all, _ := s.Query("1=1", nil, 10, 0)
	if len(all) != 2 {
		t.Fatalf("want two tasks, got %d", len(all))
	}
}

func TestSearchFindsTitle(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.Apply([]Command{
		cmd(t, "u1", "task_add", TaskArgs{ID: "t1", Title: ptr("Book the guest house")}),
		cmd(t, "u2", "task_add", TaskArgs{ID: "t2", Title: ptr("Print the route")}),
	}); err != nil {
		t.Fatal(err)
	}
	ids, err := s.Search("guest", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "t1" {
		t.Fatalf("search returned %v", ids)
	}
}

// Two clients that both write must both end at the same state, and no command
// may vanish.
func TestConcurrentClientsConverge(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.Apply([]Command{cmd(t, "seed", "task_add", TaskArgs{ID: "t1", Title: ptr("Shared")})}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	for c := 0; c < 2; c++ {
		go func(client int) {
			for i := 0; i < 20; i++ {
				u := fmt.Sprintf("c%d-%d", client, i)
				_, res, err := s.Apply([]Command{cmd(t, u, "task_update", TaskArgs{
					ID: "t1", Title: ptr(fmt.Sprintf("client %d edit %d", client, i)),
				})})
				if err != nil {
					done <- err
					return
				}
				if !res[0].OK {
					done <- fmt.Errorf("command %s failed: %s", u, res[0].Error)
					return
				}
			}
			done <- nil
		}(c)
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Task("t1")
	if err != nil {
		t.Fatal(err)
	}
	// Last write wins, so the title belongs to one of the clients, and the
	// version equals the count of applied commands.
	if got.Title == "Shared" {
		t.Fatalf("no edit landed")
	}
	var applied int
	if err := s.db.QueryRow(`SELECT count(*) FROM applied_command`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 41 {
		t.Fatalf("applied %d commands, want 41", applied)
	}
}

func TestProjectNameResolution(t *testing.T) {
	s := newStore(t)
	if _, res, err := s.Apply([]Command{
		cmd(t, "p1", "project_add", ProjectArgs{ID: "p_trip", Name: ptr("Trip to Setomaa")}),
		cmd(t, "p2", "project_add", ProjectArgs{ID: "p_shop", Name: ptr("Shopping")}),
		cmd(t, "p3", "project_add", ProjectArgs{ID: "p_shed", Name: ptr("Shed rebuild")}),
	}); err != nil {
		t.Fatal(err)
	} else {
		for _, r := range res {
			if !r.OK {
				t.Fatal(r.Error)
			}
		}
	}
	cases := []struct {
		name, want, wantErr string
	}{
		{"Trip to Setomaa", "p_trip", ""},        // exact
		{"trip", "p_trip", ""},                   // unique prefix, other case
		{"#Trip", "p_trip", ""},                  // the hash is optional
		{"inbox", InboxID, ""},                   // the inbox by name
		{"Sh", "", "matches"},                    // two projects start with Sh
		{"nothing here", "", "no project named"}, // absent
	}
	for _, c := range cases {
		_, res, err := s.Apply([]Command{cmd(t, "t-"+c.name, "task_add", TaskArgs{
			ID: "task-" + c.name, Title: ptr("x"), Project: ptr(c.name),
		})})
		if err != nil {
			t.Fatal(err)
		}
		if c.wantErr != "" {
			if res[0].OK || !strings.Contains(res[0].Error, c.wantErr) {
				t.Errorf("%q: got %+v, want an error containing %q", c.name, res[0], c.wantErr)
			}
			continue
		}
		if !res[0].OK {
			t.Errorf("%q failed: %s", c.name, res[0].Error)
			continue
		}
		got, err := s.Task("task-" + c.name)
		if err != nil {
			t.Fatal(err)
		}
		if got.ProjectID != c.want {
			t.Errorf("%q landed in %s, want %s", c.name, got.ProjectID, c.want)
		}
	}
}
