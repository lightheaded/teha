// SPDX-License-Identifier: AGPL-3.0-or-later

package store

// The three gaps in Todoist filters that §6.3 of docs/PLAN.md says teha closes
// from day one, run against real rows.
//
// filter/gaps_test.go proves that the grammar compiles. This file proves that
// the SQL answers, because a fragment that compiles and returns the wrong rows
// is worse than one that does not compile at all.

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lightheaded/teha/filter"
)

// gapsToday fixes what "today" means, so a case never drifts.
var gapsToday = time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)

// gapStore builds one small account that holds every shape the three gaps
// need: a parent with sub-tasks, a completed task, and a task that carries a
// comment.
func gapStore(t *testing.T) *Store {
	t.Helper()
	s := openStoreAt(t, filepath.Join(t.TempDir(), "gaps.db"))
	cmds := []Command{
		mkCmd("g1", "project_add", ProjectArgs{ID: "p_trip", Name: ptr("Travel"), OrderKey: ptr("V")}),

		// A parent with two sub-tasks. The parent is p1 and the sub-tasks are
		// not, which is the case Todoist answers badly.
		mkCmd("g2", "task_add", TaskArgs{ID: "t_parent", Title: ptr("Plan the ferry trip"),
			ProjectID: ptr("p_trip"), Priority: ptr(1), DueDate: ptr("2026-08-25")}),
		mkCmd("g3", "task_add", TaskArgs{ID: "t_kid1", Title: ptr("Book the cabin"),
			ProjectID: ptr("p_trip"), ParentID: ptr("t_parent"), Priority: ptr(4)}),
		mkCmd("g4", "task_add", TaskArgs{ID: "t_kid2", Title: ptr("Pack the maps"),
			ProjectID: ptr("p_trip"), ParentID: ptr("t_parent"), Priority: ptr(4)}),

		// A sub-task that matches on its own, under a parent that does not.
		mkCmd("g5", "task_add", TaskArgs{ID: "t_quiet", Title: ptr("Sort the shed"),
			ProjectID: ptr("p_trip"), Priority: ptr(4)}),
		mkCmd("g6", "task_add", TaskArgs{ID: "t_loud", Title: ptr("Fix the roof felt"),
			ProjectID: ptr("p_trip"), ParentID: ptr("t_quiet"), Priority: ptr(1)}),

		// A task and a comment on it. A comment is a row of its own, so the
		// description stays empty here: that is what makes the two roads to the
		// row, `comment:` and `search:`, tell the difference below.
		mkCmd("g7", "task_add", TaskArgs{ID: "t_comment", Title: ptr("Call the yard"),
			ProjectID: ptr("p_trip")}),
		mkCmd("g7b", "comment_add", CommentArgs{ID: "c_gate", TaskID: ptr("t_comment"),
			Body: ptr("The gate code is behind the meter.")}),

		// A completed task and a task nobody will do.
		mkCmd("g8", "task_add", TaskArgs{ID: "t_done", Title: ptr("Renew the card"),
			ProjectID: ptr("p_trip"), Labels: []string{"errand"}}),
		mkCmd("g9", "task_complete", IDArgs{ID: "t_done"}),
		mkCmd("g10", "task_add", TaskArgs{ID: "t_never", Title: ptr("Learn to sail"),
			ProjectID: ptr("p_trip")}),
		mkCmd("g11", "task_wont_do", IDArgs{ID: "t_never"}),
	}
	_, res, err := s.Apply(cmds)
	if err != nil {
		t.Fatalf("the account did not build: %v", err)
	}
	for _, r := range res {
		if !r.OK {
			t.Fatalf("the command %s failed: %s", r.UUID, r.Error)
		}
	}
	return s
}

// ask runs one query the way the server, the MCP tools and a saved view all do.
func ask(t *testing.T, s *Store, query string) []string {
	t.Helper()
	where, args, err := filter.Compile(query, gapsToday)
	if err != nil {
		t.Fatalf("%q did not compile: %v", query, err)
	}
	tasks, err := s.Query(where, args, 100, 0)
	if err != nil {
		t.Fatalf("%q did not run: %v", query, err)
	}
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.ID)
	}
	sort.Strings(out)
	return out
}

func want(ids ...string) string {
	sort.Strings(ids)
	return strings.Join(ids, " ")
}

// TestGapOneAQueryReachesCommentText is the first gap against real rows.
func TestGapOneAQueryReachesCommentText(t *testing.T) {
	s := gapStore(t)

	if got, w := strings.Join(ask(t, s, "comment: gate code"), " "), want("t_comment"); got != w {
		t.Errorf("comment: gate code returned %q, want %q", got, w)
	}
	// `search:` reads the title and the description, and a comment is neither,
	// so it must NOT find this row. The two terms mean two things now that a
	// comment is a row: docs/BACKLOG.md records that the full-text index holds
	// no comment text.
	if got := ask(t, s, "search: gate code"); len(got) != 0 {
		t.Errorf("search: gate code returned %v, and a comment is not a description", got)
	}
	// A comment search must not answer with a title match, or the term means
	// two things at once.
	if got := ask(t, s, "comment: Call the yard"); len(got) != 0 {
		t.Errorf("comment: Call the yard returned %v, and that text is a title, not a comment", got)
	}
	// Nothing at all is an empty answer, not every row.
	if got := ask(t, s, "comment: a phrase nobody wrote"); len(got) != 0 {
		t.Errorf("a comment search with no match returned %v", got)
	}
}

// TestGapTwoAQueryShowsCompletedTasks is the second gap against real rows.
// This is where the compile-level test cannot help: the narrowing to open
// tasks is added by Compile, and only a real query proves that a completed row
// comes back.
func TestGapTwoAQueryShowsCompletedTasks(t *testing.T) {
	s := gapStore(t)

	if got, w := strings.Join(ask(t, s, "done"), " "), want("t_done"); got != w {
		t.Errorf("done returned %q, want %q", got, w)
	}
	if got, w := strings.Join(ask(t, s, "completed & #Travel"), " "), want("t_done"); got != w {
		t.Errorf("completed & #Travel returned %q, want %q", got, w)
	}
	if got, w := strings.Join(ask(t, s, "done & %errand"), " "), want("t_done"); got != w {
		t.Errorf("done & %%errand returned %q, want %q", got, w)
	}
	if got, w := strings.Join(ask(t, s, "wont do"), " "), want("t_never"); got != w {
		t.Errorf("wont do returned %q, want %q", got, w)
	}

	// An ordinary query still hides both of them, which is the other half of
	// the promise: a person who asks for a project does not want a graveyard.
	for _, query := range []string{"#Travel", "search: renew", "%errand"} {
		for _, hidden := range []string{"t_done", "t_never"} {
			for _, got := range ask(t, s, query) {
				if got == hidden {
					t.Errorf("%q answered with %s, which is not open", query, hidden)
				}
			}
		}
	}

	// `any state` answers with every row, open and closed.
	all := ask(t, s, "any state")
	if len(all) != 8 {
		t.Errorf("any state returned %d rows, want all 8", len(all))
	}
}

// TestGapThreeAParentComesWithItsSubtasks is the third gap against real rows,
// in both directions.
func TestGapThreeAParentComesWithItsSubtasks(t *testing.T) {
	s := gapStore(t)

	// Todoist answers p1 with the parent alone and the loud sub-task alone.
	if got, w := strings.Join(ask(t, s, "p1"), " "), want("t_parent", "t_loud"); got != w {
		t.Errorf("p1 returned %q, want %q. This is the Todoist answer and the "+
			"baseline of the gap", got, w)
	}

	// The gap term answers with the whole family, in both directions: the
	// parent brings its two sub-tasks down, and the loud sub-task brings its
	// quiet parent up.
	got := strings.Join(ask(t, s, "with subtasks: p1"), " ")
	w := want("t_parent", "t_kid1", "t_kid2", "t_loud", "t_quiet")
	if got != w {
		t.Errorf("with subtasks: p1 returned %q, want %q", got, w)
	}

	// It composes with a project, and it never leaks a row from elsewhere.
	if got, w := strings.Join(ask(t, s, "with subtasks: p1 & #Travel"), " "),
		want("t_parent", "t_kid1", "t_kid2", "t_loud", "t_quiet"); got != w {
		t.Errorf("with subtasks: p1 & #Travel returned %q, want %q", got, w)
	}

	// A term that matches nothing brings no family with it.
	if got := ask(t, s, "with subtasks: search: a phrase nobody wrote"); len(got) != 0 {
		t.Errorf("a family of nothing returned %v", got)
	}

	// A task with no family answers with itself alone.
	if got, w := strings.Join(ask(t, s, "with subtasks: comment: gate code"), " "),
		want("t_comment"); got != w {
		t.Errorf("with subtasks over a lone task returned %q, want %q", got, w)
	}

	// A completed row still stays out unless the query asks for it, so the
	// term widens the family and never the state.
	if _, res, err := s.Apply([]Command{
		mkCmd("g12", "task_add", TaskArgs{ID: "t_kid3", Title: ptr("Old step"),
			ProjectID: ptr("p_trip"), ParentID: ptr("t_parent")}),
		mkCmd("g13", "task_complete", IDArgs{ID: "t_kid3"}),
	}); err != nil || !res[0].OK || !res[1].OK {
		t.Fatalf("the extra sub-task did not go in: %v %+v", err, res)
	}
	for _, id := range ask(t, s, "with subtasks: p1") {
		if id == "t_kid3" {
			t.Error("a completed sub-task arrived in a query that never named a state")
		}
	}
	// And with the state named, it does arrive.
	found := false
	for _, id := range ask(t, s, "with subtasks: any state & any state") {
		if id == "t_kid3" {
			found = true
		}
	}
	if !found {
		t.Error("a completed sub-task never arrives, even when the query names any state")
	}
}
