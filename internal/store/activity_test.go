// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"strings"
	"testing"
)

// The activity log answers "who did what". Four promises are worth a test: a
// command writes a line, a line is scoped exactly as the row it describes, a
// personal line stays personal, and a deleted task can still be named and
// brought back from the log.

func TestEveryCommandWritesOneLineOfTheLog(t *testing.T) {
	s := newStore(t)
	mustApply(t, s, []Command{
		cmdOf("project_add", ProjectArgs{ID: "p_home", Name: ptr("Home")}),
		cmdOf("task_add", TaskArgs{ID: "t_bin", Title: ptr("Take the bin out"), ProjectID: ptr("p_home")}),
		cmdOf("task_update", TaskArgs{ID: "t_bin", Priority: ptr(1), DueDate: ptr("2026-09-04")}),
		cmdOf("task_complete", IDArgs{ID: "t_bin"})})

	rows, err := s.ActivityFor(ActivityQuery{}, OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	// Newest first.
	want := []string{"task_complete", "task_update", "task_add", "project_add"}
	if len(rows) != len(want) {
		t.Fatalf("the log holds %d lines, want %d: %+v", len(rows), len(want), rows)
	}
	for i, action := range want {
		if rows[i].Action != action {
			t.Fatalf("line %d is %q, want %q", i, rows[i].Action, action)
		}
	}

	// The line names the row it describes, so the view needs no second query.
	if rows[0].TaskID != "t_bin" || rows[0].Title != "Take the bin out" {
		t.Fatalf("a task line must carry the id and the title: %+v", rows[0])
	}
	if rows[0].ProjectID != "p_home" {
		t.Fatalf("a task line must carry the project, got %q", rows[0].ProjectID)
	}
	if rows[3].ProjectID != "p_home" || rows[3].Title != "Home" {
		t.Fatalf("a project line must name the list: %+v", rows[3])
	}
	// An update says which fields it asked to change, so the view can be read
	// without opening the task.
	if rows[1].Detail != "due_date,priority" {
		t.Fatalf("the update line says %q, want the two field names", rows[1].Detail)
	}
	// Every line names the person, by name and not by id.
	if rows[0].AccountID != OwnerID || rows[0].Actor == "" {
		t.Fatalf("a line must name who did it: %+v", rows[0])
	}
}

func TestARefusedCommandWritesNoLine(t *testing.T) {
	s := newStore(t)
	_, res, err := s.Apply([]Command{cmdOf("task_update", TaskArgs{ID: "nothing", Title: ptr("x")})})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].OK {
		t.Fatal("a task_update against a missing task must fail")
	}
	rows, err := s.ActivityFor(ActivityQuery{}, OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("a command that was rolled back left %d lines: %+v", len(rows), rows)
	}
}

func TestTheLogIsScopedLikeTheRowsItDescribes(t *testing.T) {
	s, partner := twoAccounts(t)
	mustApplyAs(t, s, OwnerID,
		cmdOf("project_add", ProjectArgs{ID: "p_shop", Name: ptr("Shopping")}),
		cmdOf("project_add", ProjectArgs{ID: "p_secret", Name: ptr("Presents")}),
		cmdOf("task_add", TaskArgs{ID: "t_milk", Title: ptr("Milk"), ProjectID: ptr("p_shop")}),
		cmdOf("task_add", TaskArgs{ID: "t_gift", Title: ptr("A bicycle"), ProjectID: ptr("p_secret")}))

	// Nothing is shared yet, so the other person reads an empty log.
	rows, err := s.ActivityFor(ActivityQuery{}, partner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("a log of somebody else's lists must be empty, got %+v", rows)
	}

	if err := s.ShareProject("p_shop", partner.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ActivityFor(ActivityQuery{}, partner.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ProjectID == "p_secret" || r.Title == "A bicycle" {
			t.Fatalf("a line of a list out of reach leaked: %+v", r)
		}
	}
	if len(rows) == 0 {
		t.Fatal("the shared list must carry its history to the person it was shared with")
	}
}

func TestAPersonalLineStaysPersonal(t *testing.T) {
	s, partner := twoAccounts(t)
	// A login is the audit half of the log, and it belongs to one person.
	s.Note(partner.ID, ActionLogin, "The phone", "", "192.0.2.7")

	mine, err := s.ActivityFor(ActivityQuery{}, partner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || mine[0].Action != ActionLogin || mine[0].Addr != "192.0.2.7" {
		t.Fatalf("a person must read their own login: %+v", mine)
	}

	// Not even the owner of the household reads it.
	theirs, err := s.ActivityFor(ActivityQuery{}, OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range theirs {
		if r.AccountID == partner.ID {
			t.Fatalf("the owner read somebody else's personal line: %+v", r)
		}
	}
}

func TestTheLogNamesADeletedTaskAndRestoresIt(t *testing.T) {
	s := newStore(t)
	mustApply(t, s, []Command{
		cmdOf("task_add", TaskArgs{ID: "t_gone", Title: ptr("Cancel the gym")}),
		cmdOf("task_delete", IDArgs{ID: "t_gone"})})

	rows, err := s.ActivityFor(ActivityQuery{TaskID: "t_gone"}, OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Action != "task_delete" {
		t.Fatalf("the log must hold the delete: %+v", rows)
	}
	// The title survives the delete, or the log cannot say what went.
	if rows[0].Title != "Cancel the gym" {
		t.Fatalf("a delete line must name the task, got %q", rows[0].Title)
	}

	// Restore is one command away, which is the whole promise of the view.
	mustApply(t, s, []Command{cmdOf("task_restore", IDArgs{ID: "t_gone"})})
	task, err := s.TaskFor("t_gone", OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if task.DeletedAt != nil {
		t.Fatal("the task must be back")
	}
}

func TestADeletedListComesBack(t *testing.T) {
	s := newStore(t)
	mustApply(t, s, []Command{
		cmdOf("project_add", ProjectArgs{ID: "p_trip", Name: ptr("Trip")}),
		cmdOf("task_add", TaskArgs{ID: "t_pack", Title: ptr("Pack"), ProjectID: ptr("p_trip")}),
		cmdOf("project_delete", IDArgs{ID: "p_trip"})})

	mustApply(t, s, []Command{cmdOf("project_restore", IDArgs{ID: "p_trip"})})
	d, err := s.PullFor(0, OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	var back bool
	for _, p := range d.Projects {
		if p.ID == "p_trip" && p.DeletedAt == nil {
			back = true
		}
	}
	if !back {
		t.Fatal("project_restore must bring the list back")
	}
	// The tasks inside were never marked deleted, so the list comes back whole.
	tasks, err := s.QueryFor("project_id = ?", []any{"p_trip"}, 0, 0, OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("the list came back with %d tasks, want 1", len(tasks))
	}
}

func TestOnePageOfTheLogAtATime(t *testing.T) {
	s := newStore(t)
	for i := 0; i < 12; i++ {
		mustApply(t, s, []Command{cmdOf("task_add", TaskArgs{
			ID: "t_" + strings.Repeat("x", i) + "z", Title: ptr("One")})})
	}
	first, err := s.ActivityFor(ActivityQuery{Limit: 5}, OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 5 {
		t.Fatalf("a page of five holds %d", len(first))
	}
	next, err := s.ActivityFor(ActivityQuery{Limit: 5, Before: first[4].Seq}, OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 5 {
		t.Fatalf("the second page holds %d, want 5", len(next))
	}
	if next[0].Seq >= first[4].Seq {
		t.Fatal("the second page must start below the first")
	}
}
