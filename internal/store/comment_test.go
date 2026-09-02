// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"strings"
	"testing"

	"github.com/lightheaded/teha/filter"
)

// A comment is a line of talk on one task. Two promises are worth a test: it
// reaches everybody who can see the task, and only its author can change it.

func TestACommentReachesEverybodyWhoSeesTheTask(t *testing.T) {
	s, partner := twoAccounts(t)
	mustApplyAs(t, s, OwnerID,
		cmdOf("project_add", ProjectArgs{ID: "p_shop", Name: ptr("Shopping")}),
		cmdOf("task_add", TaskArgs{ID: "t_milk", Title: ptr("Milk"), ProjectID: ptr("p_shop")}),
		cmdOf("comment_add", CommentArgs{ID: "cm_1", TaskID: ptr("t_milk"),
			Body: ptr("The green one, not the blue.")}))

	// Before the list is shared, the line is the owner's alone.
	if got := commentBodies(t, s, "t_milk", OwnerID); len(got) != 1 {
		t.Fatalf("the owner must see their own comment, got %v", got)
	}
	if _, err := s.CommentsFor("t_milk", partner.ID); err == nil {
		t.Fatal("a comment on a task out of reach must not be readable")
	}

	if err := s.ShareProject("p_shop", partner.ID); err != nil {
		t.Fatal(err)
	}
	if got := commentBodies(t, s, "t_milk", partner.ID); len(got) != 1 {
		t.Fatalf("a shared task carries its comments, got %v", got)
	}

	// The other person answers, and the owner reads the answer.
	mustApplyAs(t, s, partner.ID,
		cmdOf("comment_add", CommentArgs{ID: "cm_2", TaskID: ptr("t_milk"), Body: ptr("Got it.")}))
	got := commentBodies(t, s, "t_milk", OwnerID)
	if len(got) != 2 || got[1] != "Got it." {
		t.Fatalf("both lines must be there, oldest first: %v", got)
	}

	// A pull carries the comment, so a client never asks for one by hand.
	d, err := s.PullFor(0, partner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Comments) != 2 {
		t.Fatalf("the pull carried %d comments, want 2", len(d.Comments))
	}
	if d.Comments[0].AccountID != OwnerID {
		t.Fatalf("a comment must name its author, got %q", d.Comments[0].AccountID)
	}
}

func TestOnlyTheAuthorChangesACommentAndADeleteHidesIt(t *testing.T) {
	s, partner := twoAccounts(t)
	mustApplyAs(t, s, OwnerID,
		cmdOf("project_add", ProjectArgs{ID: "p_shop", Name: ptr("Shopping")}),
		cmdOf("task_add", TaskArgs{ID: "t_milk", Title: ptr("Milk"), ProjectID: ptr("p_shop")}),
		cmdOf("comment_add", CommentArgs{ID: "cm_1", TaskID: ptr("t_milk"), Body: ptr("Mine to say")}))
	if err := s.ShareProject("p_shop", partner.ID); err != nil {
		t.Fatal(err)
	}

	// The other person shares the list and still does not speak for the owner.
	for _, c := range []Command{
		cmdOf("comment_update", CommentArgs{ID: "cm_1", Body: ptr("I never said this")}),
		cmdOf("comment_delete", IDArgs{ID: "cm_1"}),
	} {
		res := applyAs(t, s, partner.ID, c)
		if res[0].OK {
			t.Fatalf("%s changed a comment that belongs to somebody else", c.Type)
		}
		if !strings.Contains(res[0].Error, "cannot reach") {
			t.Fatalf("the refusal must be one sentence, got %q", res[0].Error)
		}
	}

	// The author changes their own line.
	mustApplyAs(t, s, OwnerID, cmdOf("comment_update", CommentArgs{ID: "cm_1", Body: ptr("Said better")}))
	if got := commentBodies(t, s, "t_milk", OwnerID); len(got) != 1 || got[0] != "Said better" {
		t.Fatalf("the author must be able to edit, got %v", got)
	}

	// A delete hides the line from a read and still reaches a pull, so a
	// client that holds a copy learns to drop it.
	mustApplyAs(t, s, OwnerID, cmdOf("comment_delete", IDArgs{ID: "cm_1"}))
	if got := commentBodies(t, s, "t_milk", OwnerID); len(got) != 0 {
		t.Fatalf("a deleted comment must not be read back, got %v", got)
	}
	d, err := s.PullFor(0, partner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Comments) != 1 || d.Comments[0].DeletedAt == nil {
		t.Fatalf("a pull must carry the delete, got %+v", d.Comments)
	}
}

func TestACommentNeedsATaskAndABody(t *testing.T) {
	s := newStore(t)
	mustApplyAs(t, s, OwnerID,
		cmdOf("task_add", TaskArgs{ID: "t_one", Title: ptr("A task")}))

	for _, bad := range []Command{
		cmdOf("comment_add", CommentArgs{ID: "cm_x", Body: ptr("no task")}),
		cmdOf("comment_add", CommentArgs{ID: "cm_x", TaskID: ptr("t_missing"), Body: ptr("no such task")}),
		cmdOf("comment_add", CommentArgs{ID: "cm_x", TaskID: ptr("t_one"), Body: ptr("   ")}),
		cmdOf("comment_update", CommentArgs{ID: "cm_x", Body: ptr("nothing to change")}),
	} {
		if res := applyAs(t, s, OwnerID, bad); res[0].OK {
			t.Fatalf("%s with bad arguments was accepted", bad.Type)
		}
	}
}

// Taking an assignee away must work, and it must leave one value in the
// column. The browser clears the field and the phone sends an empty string, so
// the store has to read both as nobody.
func TestATaskCanBeUnassignedFromEitherClient(t *testing.T) {
	s, partner := twoAccounts(t)
	mustApplyAs(t, s, OwnerID,
		cmdOf("project_add", ProjectArgs{ID: "p_shop", Name: ptr("Shopping")}),
		cmdOf("task_add", TaskArgs{ID: "t_milk", Title: ptr("Milk"), ProjectID: ptr("p_shop")}))
	if err := s.ShareProject("p_shop", partner.ID); err != nil {
		t.Fatal(err)
	}

	// The browser: a clear list.
	mustApplyAs(t, s, OwnerID,
		cmdOf("task_update", TaskArgs{ID: "t_milk", AssigneeID: ptr(partner.ID)}))
	if got := oneTask(t, s, "t_milk").AssigneeID; got == nil || *got != partner.ID {
		t.Fatalf("the assignee did not stick: %v", got)
	}
	mustApplyAs(t, s, OwnerID,
		cmdOf("task_update", TaskArgs{ID: "t_milk", Clear: []string{"assignee_id"}}))
	if got := oneTask(t, s, "t_milk").AssigneeID; got != nil {
		t.Fatalf("a clear left %q behind, want nobody", *got)
	}

	// The phone: an empty string. It has to mean the same thing, and it has to
	// leave the column NULL rather than a second way of saying nobody.
	mustApplyAs(t, s, OwnerID,
		cmdOf("task_update", TaskArgs{ID: "t_milk", AssigneeID: ptr(partner.ID)}),
		cmdOf("task_update", TaskArgs{ID: "t_milk", AssigneeID: ptr("")}))
	if got := oneTask(t, s, "t_milk").AssigneeID; got != nil {
		t.Fatalf("an empty assignee left %q behind, want nobody", *got)
	}

	// `unassigned` finds it, which is the term a shared list is read with.
	schema := filter.ServerSchema
	schema.Me = OwnerID
	where, args, err := filter.CompileFor("unassigned", s.Now(), schema)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.QueryFor(where, args, 50, 0, OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.ID == "t_milk" {
			found = true
		}
	}
	if !found {
		t.Fatal("unassigned must find a task that nobody has")
	}
}

// A task can only be given to somebody who can see the list it is in. The
// refusal matters twice: they could never open the task, and the notification
// about it would carry a title they may not read.
func TestATaskCannotBeGivenToSomebodyOutOfReach(t *testing.T) {
	s, partner := twoAccounts(t)
	mustApplyAs(t, s, OwnerID,
		cmdOf("project_add", ProjectArgs{ID: "p_mine", Name: ptr("Private")}),
		cmdOf("task_add", TaskArgs{ID: "t_solo", Title: ptr("My own"), ProjectID: ptr("p_mine")}))

	for _, bad := range []Command{
		cmdOf("task_update", TaskArgs{ID: "t_solo", AssigneeID: ptr(partner.ID)}),
		cmdOf("task_add", TaskArgs{ID: "t_two", Title: ptr("Also mine"),
			ProjectID: ptr("p_mine"), AssigneeID: ptr(partner.ID)}),
		cmdOf("task_add", TaskArgs{ID: "t_three", Title: ptr("For a stranger"),
			ProjectID: ptr("p_mine"), AssigneeID: ptr("a_nobody")}),
	} {
		res := applyAs(t, s, OwnerID, bad)
		if res[0].OK {
			t.Fatalf("%s gave a task to somebody who cannot see the list", bad.Type)
		}
		if !strings.Contains(res[0].Error, "cannot") {
			t.Fatalf("the refusal must be one sentence, got %q", res[0].Error)
		}
	}
	if got := oneTask(t, s, "t_solo").AssigneeID; got != nil {
		t.Fatalf("the assignee was written anyway: %q", *got)
	}

	// Sharing the list makes the same command work.
	if err := s.ShareProject("p_mine", partner.ID); err != nil {
		t.Fatal(err)
	}
	mustApplyAs(t, s, OwnerID,
		cmdOf("task_update", TaskArgs{ID: "t_solo", AssigneeID: ptr(partner.ID)}))
	if got := oneTask(t, s, "t_solo").AssigneeID; got == nil || *got != partner.ID {
		t.Fatalf("the assignee did not stick after the share: %v", got)
	}
}

func commentBodies(t *testing.T, s *Store, taskID, accountID string) []string {
	t.Helper()
	rows, err := s.CommentsFor(taskID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(rows))
	for _, c := range rows {
		out = append(out, c.Body)
	}
	return out
}
