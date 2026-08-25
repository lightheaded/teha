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
	if sql != "1=1" {
		t.Fatalf("empty query compiled to %q", sql)
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
