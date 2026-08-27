// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"strings"
	"testing"
	"time"
)

// today is a Tuesday, so weekday arithmetic is easy to read in the cases.
var today = time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)

func TestCompileShape(t *testing.T) {
	cases := []struct {
		query    string
		wantSQL  []string // fragments that must appear
		wantArgs []any
	}{
		{"today", []string{"due_date <= ?", "state = 'open'"}, []any{"2026-08-25"}},
		{"tomorrow", []string{"due_date = ?"}, []any{"2026-08-26"}},
		{"overdue", []string{"due_date < ?"}, []any{"2026-08-25"}},
		{"od", []string{"due_date < ?"}, []any{"2026-08-25"}},
		{"no date", []string{"due_date IS NULL"}, nil},
		{"p1", []string{"priority = ?"}, []any{1}},
		{"no priority", []string{"priority = 4"}, nil},
		{"recurring", []string{"rrule IS NOT NULL"}, nil},
		{"subtask", []string{"parent_id IS NOT NULL"}, nil},
		{"deferred", []string{"start_date IS NOT NULL", "start_date > ?"}, []any{"2026-08-25"}},
		{"done", []string{"state = 'done'"}, nil},
		{"#Home", []string{"project_id IN (SELECT id FROM project"}, []any{"Home"}},
		{"#inbox", []string{"project_id = 'inbox'"}, nil},
		{"##Home", []string{"WITH RECURSIVE tree"}, []any{"Home"}},
		{"/Errands", []string{"section_id IN (SELECT id FROM section"}, []any{"Errands"}},
		{"no section", []string{"section_id IS NULL"}, nil},
		{"%store", []string{"task_label"}, []any{"store"}},
		{"@store", []string{"task_label"}, []any{"store"}},
		{"search: milk", []string{"lower(title) LIKE ?"}, []any{"%milk%", "%milk%"}},
		{"before: today", []string{"due_date < ?"}, []any{"2026-08-25"}},
		{"after: 3 days", []string{"due_date > ?"}, []any{"2026-08-28"}},
		{"deadline: friday", []string{"deadline = ?"}, []any{"2026-08-28"}},
		{"date: 24.12.2026", []string{"due_date = ?"}, []any{"2026-12-24"}},
	}
	for _, c := range cases {
		sql, args, err := Compile(c.query, today)
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.query, err)
			continue
		}
		for _, want := range c.wantSQL {
			if !strings.Contains(sql, want) {
				t.Errorf("%q: SQL %q does not contain %q", c.query, sql, want)
			}
		}
		if len(c.wantArgs) != len(args) {
			t.Errorf("%q: got %d args %v, want %d %v", c.query, len(args), args, len(c.wantArgs), c.wantArgs)
			continue
		}
		for i := range args {
			if args[i] != c.wantArgs[i] {
				t.Errorf("%q: arg %d is %v, want %v", c.query, i, args[i], c.wantArgs[i])
			}
		}
	}
}

func TestCompileOperators(t *testing.T) {
	cases := []struct{ query, want string }{
		{"today & p1", " AND "},
		{"today | tomorrow", " OR "},
		{"today, tomorrow", " OR "}, // a comma means several lists, so OR
		{"!recurring", "NOT ("},
		{"(today | tomorrow) & #Home", " AND "},
	}
	for _, c := range cases {
		sql, _, err := Compile(c.query, today)
		if err != nil {
			t.Errorf("%q: %v", c.query, err)
			continue
		}
		if !strings.Contains(sql, c.want) {
			t.Errorf("%q compiled to %q, want %q inside", c.query, sql, c.want)
		}
	}
}

func TestCompileRejects(t *testing.T) {
	for _, q := range []string{"today & (", ")", "today &", "before: never"} {
		if _, _, err := Compile(q, today); err == nil {
			t.Errorf("%q compiled, want an error", q)
		}
	}
}

func TestEmptyQueryMatchesEverythingOpen(t *testing.T) {
	sql, args, err := Compile("", today)
	if err != nil || len(args) != 0 {
		t.Fatalf("empty query: sql=%q args=%v err=%v", sql, args, err)
	}
	// This test asserted "1=1" until 2026-08-25, which is what its own name
	// says it must not be. An empty query returned early, before the narrowing
	// step, so `list_tasks` with no filter and `teha ls` with no argument both
	// answered with completed rows mixed in.
	if sql != "state = 'open'" {
		t.Fatalf("empty query compiled to %q", sql)
	}
}

// The narrowing must key off what the parser saw, not off the query text.
// A search for the word done, and a project whose name contains it, both used
// to switch the narrowing off and mix completed rows into an ordinary view.
func TestStateNarrowingIgnoresTextThatOnlyLooksLikeState(t *testing.T) {
	for _, q := range []string{"search: done", "#Done", "search: completed", "%done"} {
		sql, _, err := Compile(q, today)
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		if !strings.Contains(sql, "state = 'open'") {
			t.Errorf("%q compiled to %q, which does not narrow to open tasks", q, sql)
		}
	}
}

// A query that names a state must keep it, and must not be narrowed on top.
func TestExplicitStateTermsAreKept(t *testing.T) {
	cases := map[string]string{
		"done":      "state = 'done'",
		"completed": "state = 'done'",
		"wont do":   "state = 'wont_do'",
		"skipped":   "state = 'wont_do'",
		"open":      "state = 'open'",
	}
	for q, want := range cases {
		sql, _, err := Compile(q, today)
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		if !strings.Contains(sql, want) {
			t.Errorf("%q compiled to %q, want it to contain %q", q, sql, want)
		}
		if q != "open" && strings.Contains(sql, "state = 'open'") {
			t.Errorf("%q compiled to %q, which narrows a state query to open tasks", q, sql)
		}
	}
}

// A filter that asks for completed tasks must not be narrowed to open ones.
func TestDoneQueryKeepsCompleted(t *testing.T) {
	sql, _, err := Compile("done & #Home", today)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, "state = 'open'") {
		t.Fatalf("a done query was narrowed to open tasks: %s", sql)
	}
}

// A section name is data. It holds spaces, one name is the start of another,
// and a name can hold the character that LIKE reads as a wildcard. All three
// forms have to reach the database as text, and only a trailing * may widen
// the match. These fixtures lock that.
func TestSectionNames(t *testing.T) {
	cases := []struct {
		query   string
		wantSQL string
		notSQL  string
		wantArg any
	}{
		// A name with a space. The lexer splits on the operators only, so the
		// whole phrase is one term.
		{"/Next actions", "lower(name) = lower(?)", "LIKE", "Next actions"},
		// A name that is the start of another one. The exact form compares with
		// =, so /Errand must never reach the section called Errands.
		{"/Errand", "lower(name) = lower(?)", "LIKE", "Errand"},
		// A trailing * is the one form that widens the match, exactly as
		// #Project* does.
		{"/Errand*", "lower(name) LIKE lower(?)", " = lower(?)", "Errand%"},
		// A name that holds a wildcard character. The exact form compares with
		// =, so the % is literal text and not a wildcard.
		{"/50% done", "lower(name) = lower(?)", "LIKE", "50% done"},
		// An underscore is the other LIKE wildcard, and it is literal here too.
		{"/read_me", "lower(name) = lower(?)", "LIKE", "read_me"},
	}
	for _, c := range cases {
		sql, args, err := Compile(c.query, today)
		if err != nil {
			t.Errorf("%q: %v", c.query, err)
			continue
		}
		if !strings.Contains(sql, c.wantSQL) {
			t.Errorf("%q compiled to %q, want %q inside", c.query, sql, c.wantSQL)
		}
		if strings.Contains(sql, c.notSQL) {
			t.Errorf("%q compiled to %q, which must not contain %q", c.query, sql, c.notSQL)
		}
		if len(args) != 1 || args[0] != c.wantArg {
			t.Errorf("%q gave args %v, want [%v]", c.query, args, c.wantArg)
		}
	}
}

// A section term narrows to open tasks like every other place term, and it
// combines with the operators.
func TestSectionCombines(t *testing.T) {
	sql, args, err := Compile("#Home & /Errands & !no section", today)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"project", "section", " AND ", "NOT (", "state = 'open'"} {
		if !strings.Contains(sql, want) {
			t.Errorf("%q is missing from %q", want, sql)
		}
	}
	if len(args) != 2 || args[0] != "Home" || args[1] != "Errands" {
		t.Errorf("args are %v, want [Home Errands]", args)
	}
}
