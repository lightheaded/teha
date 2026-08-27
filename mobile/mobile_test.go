// SPDX-License-Identifier: Apache-2.0

package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not JSON: %v (%s)", err, s)
	}
	return m
}

func TestParseQuickAdd(t *testing.T) {
	got := decode(t, ParseQuickAdd("Book the ferry tomorrow at 9:30 p1 #Trip @call", "2026-08-25"))
	if got["title"] != "Book the ferry" {
		t.Errorf("title = %q", got["title"])
	}
	if got["due"] != "2026-08-26" {
		t.Errorf("due = %q", got["due"])
	}
	if got["time"] != "09:30" {
		t.Errorf("time = %q", got["time"])
	}
	if got["priority"] != float64(1) {
		t.Errorf("priority = %v", got["priority"])
	}
	if got["project"] != "Trip" {
		t.Errorf("project = %q", got["project"])
	}
}

// A line with no fields must still return every key, so that a client reads the
// same shape every time and needs no null checks.
func TestParseQuickAddEmptyFields(t *testing.T) {
	got := decode(t, ParseQuickAdd("Just a title", "2026-08-25"))
	for _, k := range []string{"title", "due", "time", "priority", "project", "labels", "rrule", "parsed"} {
		if _, ok := got[k]; !ok {
			t.Errorf("key %q is missing", k)
		}
	}
	if l, ok := got["labels"].([]any); !ok || len(l) != 0 {
		t.Errorf("labels = %v, want an empty list", got["labels"])
	}
}

func TestParseQuickAddBadDate(t *testing.T) {
	got := decode(t, ParseQuickAdd("anything", "not-a-day"))
	if _, ok := got["error"]; !ok {
		t.Errorf("want an error key, got %v", got)
	}
}

func TestCompileFilter(t *testing.T) {
	got := decode(t, CompileFilter("today", "2026-08-25"))
	sql, ok := got["sql"].(string)
	if !ok || sql == "" {
		t.Fatalf("sql = %v", got["sql"])
	}
	if _, ok := got["args"]; !ok {
		t.Error("args key is missing")
	}
}

func TestCompileFilterRejects(t *testing.T) {
	got := decode(t, CompileFilter("today &", "2026-08-25"))
	if _, ok := got["error"]; !ok {
		t.Errorf("want an error key, got %v", got)
	}
}

// The phone reads its own Room database, so the clause must name the Room
// columns and never the server ones.
func TestCompileFilterRoom(t *testing.T) {
	got := decode(t, CompileFilterRoom("today & %work", "2026-08-25"))
	sql, ok := got["sql"].(string)
	if !ok || sql == "" {
		t.Fatalf("sql = %v", got["sql"])
	}
	for _, want := range []string{"dueDate", "labels"} {
		if !strings.Contains(sql, want) {
			t.Errorf("sql %q does not name %q", sql, want)
		}
	}
	for _, banned := range []string{"due_date", "task_label"} {
		if strings.Contains(sql, banned) {
			t.Errorf("sql %q names the server column %q", sql, banned)
		}
	}
	args, ok := got["args"].([]any)
	if !ok || len(args) != 2 {
		t.Fatalf("args = %v, want the day and the label pattern", got["args"])
	}
}

func TestCompileFilterRoomRejects(t *testing.T) {
	// The Room database keeps no creation date, so this term cannot work
	// there. The message says so, and no SQL comes back.
	got := decode(t, CompileFilterRoom("created: today", "2026-08-25"))
	if _, ok := got["error"]; !ok {
		t.Errorf("want an error key, got %v", got)
	}
	got = decode(t, CompileFilterRoom("today &", "2026-08-25"))
	if _, ok := got["error"]; !ok {
		t.Errorf("want an error key, got %v", got)
	}
	got = decode(t, CompileFilterRoom("today", "not a day"))
	if _, ok := got["error"]; !ok {
		t.Errorf("want an error key, got %v", got)
	}
}

func TestNextRecurrence(t *testing.T) {
	got := decode(t, NextRecurrence("FREQ=WEEKLY", "2026-08-25", "2026-08-25", false))
	if got["due"] != "2026-09-01" {
		t.Errorf("due = %v", got["due"])
	}
}

func TestNextRecurrenceRejects(t *testing.T) {
	got := decode(t, NextRecurrence("NOT A RULE", "2026-08-25", "2026-08-25", false))
	if _, ok := got["error"]; !ok {
		t.Errorf("want an error key, got %v", got)
	}
}

func TestValidRecurrence(t *testing.T) {
	if got := decode(t, ValidRecurrence("FREQ=DAILY")); got["valid"] != true {
		t.Errorf("valid = %v", got["valid"])
	}
	if got := decode(t, ValidRecurrence("nonsense")); got["valid"] != false {
		t.Errorf("valid = %v", got["valid"])
	}
}

// The identifier must sort by time and must stay unique inside one millisecond.
func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	prev := ""
	for i := 0; i < 5000; i++ {
		v := NewID("t")
		if !strings.HasPrefix(v, "t") {
			t.Fatalf("missing prefix: %q", v)
		}
		if seen[v] {
			t.Fatalf("collision after %d draws: %q", i, v)
		}
		seen[v] = true
		if v < prev {
			t.Fatalf("out of order: %q after %q", v, prev)
		}
		prev = v
	}
}
