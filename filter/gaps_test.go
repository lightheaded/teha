// SPDX-License-Identifier: Apache-2.0

package filter

// §6.3 of docs/PLAN.md names three gaps in Todoist filters that teha closes
// from day one: query comment text, show completed tasks, and show a parent
// task together with its sub-tasks in one result.
//
// Each of the three needs a test that can fail. This file holds the grammar
// half. internal/store/gaps_test.go holds the half that runs the SQL against
// real rows, because a fragment that compiles and answers wrongly is worse
// than one that does not compile.

import (
	"strings"
	"testing"
)

// --- gap one: query comment text --------------------------------------------

// TestGapOneCommentTextIsSearchable covers the first gap as far as the schema
// allows today.
//
// There is no comment table. The importer folds a Todoist comment into the
// description, and so does every client, so the description is where comment
// text lives and `comment:` searches it. The term exists so that a saved
// filter says what it means, and so that the day the table arrives there is
// one place to change. docs/BACKLOG.md records the table.
func TestGapOneCommentTextIsSearchable(t *testing.T) {
	for _, query := range []string{"comment: fridge", "note: fridge"} {
		sql, args, err := Compile(query, today)
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		if !strings.Contains(sql, "lower(description) LIKE ?") {
			t.Errorf("%q compiled to %q, which does not reach the comment text", query, sql)
		}
		if len(args) != 1 || args[0] != "%fridge%" {
			t.Errorf("%q gave args %v, want [%%fridge%%]", query, args)
		}
		// A comment search is still a search of open tasks unless the query
		// says otherwise.
		if !strings.Contains(sql, "state = 'open'") {
			t.Errorf("%q compiled to %q, which does not narrow to open tasks", query, sql)
		}
	}

	// The term composes like any other.
	sql, args, err := Compile("comment: ferry & #Travel", today)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, " AND ") {
		t.Errorf("a comment search does not compose: %q", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want the comment text and the project name", args)
	}
}

// --- gap two: show completed tasks ------------------------------------------

// TestGapTwoCompletedTasksAreReachable covers the second gap. Todoist has no
// filter term for a completed task, so a person cannot ask "what did I finish
// in this project". Four spellings answer here, and none of them may be
// narrowed back to open tasks.
func TestGapTwoCompletedTasksAreReachable(t *testing.T) {
	cases := map[string]string{
		"done":        "state = 'done'",
		"completed":   "state = 'done'",
		"wont do":     "state = 'wont_do'",
		"skipped":     "state = 'wont_do'",
		"any state":   "1=1",
		"all states":  "1=1",
		"done & #Old": "state = 'done'",
	}
	for query, want := range cases {
		sql, _, err := Compile(query, today)
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		if !strings.Contains(sql, want) {
			t.Errorf("%q compiled to %q, want %q inside", query, sql, want)
		}
		if strings.Contains(sql, "AND state = 'open'") {
			t.Errorf("%q compiled to %q, which narrows a state query back to open tasks", query, sql)
		}
	}

	// A completed task inside a wider query keeps both halves.
	sql, args, err := Compile("(done | wont do) & %errand", today)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"state = 'done'", "state = 'wont_do'", "task_label"} {
		if !strings.Contains(sql, want) {
			t.Errorf("the query lost %q: %s", want, sql)
		}
	}
	if len(args) != 1 || args[0] != "errand" {
		t.Errorf("args = %v, want [errand]", args)
	}
}

// --- gap three: a parent with its sub-tasks ---------------------------------

// TestGapThreeAParentArrivesWithItsSubtasks covers the third gap. Todoist
// answers a filter with the matching row alone: a p1 sub-task arrives without
// the parent that gives it meaning, and a p1 parent arrives without the
// sub-tasks that hold the work.
func TestGapThreeAParentArrivesWithItsSubtasks(t *testing.T) {
	for _, query := range []string{"with subtasks: p1", "with sub-tasks: p1", "family: p1"} {
		sql, args, err := Compile(query, today)
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		// The answer reaches down to a sub-task and up to a parent.
		if !strings.Contains(sql, "parent_id IN (SELECT id FROM task WHERE") {
			t.Errorf("%q does not reach down to the sub-tasks: %s", query, sql)
		}
		if !strings.Contains(sql, "id IN (SELECT parent_id FROM task WHERE") {
			t.Errorf("%q does not reach up to the parent: %s", query, sql)
		}
		// One argument per copy of the inner term, and no more.
		if len(args) != 3 {
			t.Errorf("%q gave %d args %v, want 3, one per copy of the inner term",
				query, len(args), args)
		}
		for _, a := range args {
			if a != 1 {
				t.Errorf("%q gave the argument %v, want the priority 1", query, a)
			}
		}
	}

	// The term carries a term with no argument too.
	if _, args, err := Compile("with subtasks: no date", today); err != nil || len(args) != 0 {
		t.Errorf("with subtasks: no date gave args %v and err %v", args, err)
	}
	// A term the grammar cannot read is an error, not a silent bare word search.
	if _, _, err := Compile("with subtasks: before: never", today); err == nil {
		t.Error("an unreadable inner term compiled")
	}
	// It composes with the other two gaps, which is the point of putting all
	// three in one grammar.
	sql, _, err := Compile("with subtasks: done", today)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, "AND state = 'open'") {
		t.Errorf("a family of completed tasks was narrowed to open tasks: %s", sql)
	}
}
