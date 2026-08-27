// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lightheaded/teha/filter"
)

// oldSchema returns schema.sql as a build before the section table wrote it:
// no section table, and therefore no task.section_id either, because that
// column arrives through Store.migrate and not through the file.
func oldSchema(t *testing.T) string {
	t.Helper()
	i := strings.Index(schemaSQL, "CREATE TABLE IF NOT EXISTS section")
	if i < 0 {
		t.Fatal("schema.sql no longer holds the section table")
	}
	return schemaSQL[:i]
}

// TestUpgradeOfADatabaseWithoutTheColumn is the test that the migration exists
// for. It builds a file the way an older build did, fills it, opens it with the
// current store, and asks for every row back.
func TestUpgradeOfADatabaseWithoutTheColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	// --- a file from before the column --------------------------------------
	raw, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(oldSchema(t)); err != nil {
		t.Fatal(err)
	}
	stamps := "2026-08-20T09:00:00Z"
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := raw.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO change_log (tbl, row_id, op, at) VALUES ('project','inbox','insert',?)`, stamps)
	exec(`INSERT INTO project (id, name, color, parent_id, order_key, is_inbox, created_at, updated_at, version)
		VALUES ('inbox','Inbox','grey',NULL,'m',1,?,?,1)`, stamps, stamps)
	exec(`INSERT INTO change_log (tbl, row_id, op, at) VALUES ('project','p_old','insert',?)`, stamps)
	exec(`INSERT INTO project (id, name, color, parent_id, order_key, is_inbox, created_at, updated_at, version)
		VALUES ('p_old','Garden','green',NULL,'m',0,?,?,2)`, stamps, stamps)
	exec(`INSERT INTO change_log (tbl, row_id, op, at) VALUES ('label','l_old','insert',?)`, stamps)
	exec(`INSERT INTO label (id, name, color, created_at, updated_at, version)
		VALUES ('l_old','store','grey',?,?,3)`, stamps, stamps)
	exec(`INSERT INTO change_log (tbl, row_id, op, at) VALUES ('task','t_old','insert',?)`, stamps)
	exec(`INSERT INTO task (id, project_id, parent_id, order_key, title, description, priority,
		due_date, due_time, state, source_ref, created_at, updated_at, version)
		VALUES ('t_old','p_old',NULL,'m','Order gravel','Two cubic metres.',2,
		'2026-08-24','08:30','open','todoist:5001',?,?,4)`, stamps, stamps)
	exec(`INSERT INTO task_label (task_id, label_id) VALUES ('t_old','l_old')`)
	exec(`INSERT INTO task_fts (task_id, title, description) VALUES ('t_old','Order gravel','Two cubic metres.')`)
	exec(`INSERT INTO applied_command (uuid, at, version) VALUES ('u_old',?,4)`, stamps)

	var n int
	if err := raw.QueryRow(`SELECT count(*) FROM pragma_table_info('task') WHERE name = 'section_id'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the old file already carries section_id, so this test proves nothing")
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	// --- the current store opens it ----------------------------------------
	s, err := Open(path)
	if err != nil {
		t.Fatalf("the upgrade failed: %v", err)
	}
	defer s.Close()
	s.Now = func() time.Time { return time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC) }

	// The version counter must not move. An upgrade that bumped it would make
	// every client pull the whole account again.
	v, err := s.Version()
	if err != nil {
		t.Fatal(err)
	}
	if v != 4 {
		t.Errorf("the version after the upgrade is %d, want 4", v)
	}

	d, err := s.Pull(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 1 || len(d.Projects) != 2 || len(d.Labels) != 1 {
		t.Fatalf("after the upgrade: %d tasks, %d projects, %d labels",
			len(d.Tasks), len(d.Projects), len(d.Labels))
	}
	got := d.Tasks[0]
	if got.ID != "t_old" || got.Title != "Order gravel" || got.Description != "Two cubic metres." {
		t.Errorf("the task lost a field: %+v", got)
	}
	if got.Priority != 2 || got.ProjectID != "p_old" || got.State != "open" {
		t.Errorf("the task lost a field: %+v", got)
	}
	if got.DueDate == nil || *got.DueDate != "2026-08-24" || got.DueTime == nil || *got.DueTime != "08:30" {
		t.Errorf("the due date or the time is gone: %+v", got)
	}
	if got.SourceRef == nil || *got.SourceRef != "todoist:5001" {
		t.Errorf("source_ref is gone: %+v", got)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "store" {
		t.Errorf("the label is gone: %v", got.Labels)
	}
	// The new column reads as NULL, which is what the task was before the
	// column existed: in a project, in no section.
	if got.SectionID != nil {
		t.Errorf("section of the old task = %v, want none", *got.SectionID)
	}
	// The full-text index is untouched, because ADD COLUMN rewrites no row.
	ids, err := s.Search("gravel", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "t_old" {
		t.Errorf("the search index did not survive: %v", ids)
	}
	// A command that the old file had applied must still be known, so a client
	// that retries after the upgrade does not write a second copy.
	if _, res, err := s.Apply([]Command{
		cmd(t, "u_old", "task_delete", IDArgs{ID: "t_old"}),
	}); err != nil || !res[0].OK {
		t.Fatalf("the replay guard broke: %v %v", err, res)
	}
	if _, err := s.Task("t_old"); err != nil {
		t.Errorf("a replayed command was applied after the upgrade: %v", err)
	}

	// --- and the new feature works on the upgraded file ---------------------
	_, res, err := s.Apply([]Command{
		cmd(t, "u1", "section_add", SectionArgs{ID: "s1", ProjectID: ptr("p_old"), Name: ptr("Spring")}),
		cmd(t, "u2", "task_move", MoveArgs{ID: "t_old", ProjectID: ptr("p_old"), SectionID: ptr("s1")}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range res {
		if !r.OK {
			t.Fatalf("command %d failed: %s", i, r.Error)
		}
	}
	moved, err := s.Task("t_old")
	if err != nil {
		t.Fatal(err)
	}
	if moved.SectionID == nil || *moved.SectionID != "s1" {
		t.Errorf("the task did not reach the new section: %+v", moved.SectionID)
	}

	// Opening the same file twice must not run the ALTER again.
	s.Close()
	again, err := Open(path)
	if err != nil {
		t.Fatalf("the second open failed: %v", err)
	}
	defer again.Close()
	if v2, err := again.Version(); err != nil || v2 != 6 {
		t.Errorf("the version after the second open = %d (%v), want 6", v2, err)
	}
}

// TestSectionCommands walks every section command and reads the change log
// after each one, because a client pulls by version and a command that writes a
// row without a change_log entry is a row no client ever sees.
func TestSectionCommands(t *testing.T) {
	s := newStore(t)
	_, res, err := s.Apply([]Command{
		cmd(t, "p1", "project_add", ProjectArgs{ID: "p_trip", Name: ptr("Trip")}),
		cmd(t, "s1", "section_add", SectionArgs{ID: "x1", ProjectID: ptr("p_trip"), Name: ptr("  Plan  "), OrderKey: ptr("m000010")}),
		cmd(t, "s2", "section_add", SectionArgs{ID: "x2", ProjectID: ptr("p_trip"), Name: ptr("Pack"), OrderKey: ptr("m000020")}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range res {
		if !r.OK {
			t.Fatalf("command %d failed: %s", i, r.Error)
		}
	}
	if res[1].ID != "x1" {
		t.Errorf("section_add returned the id %q, want x1", res[1].ID)
	}
	secs, err := s.Sections()
	if err != nil {
		t.Fatal(err)
	}
	if len(secs) != 2 || secs[0].Name != "Plan" || secs[1].Name != "Pack" {
		t.Fatalf("sections = %+v, want Plan then Pack with the name trimmed", secs)
	}
	if secs[0].ProjectID != "p_trip" {
		t.Errorf("the section is in project %q", secs[0].ProjectID)
	}

	// Every change reaches the change log, so a pull above a version answers.
	before, _ := s.Version()
	if _, res, err := s.Apply([]Command{
		cmd(t, "s3", "section_update", SectionArgs{ID: "x1", Name: ptr("Planning")}),
	}); err != nil || !res[0].OK {
		t.Fatalf("section_update: %v %v", err, res)
	}
	d, err := s.Pull(before)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Sections) != 1 || d.Sections[0].Name != "Planning" {
		t.Fatalf("the pull after a rename = %+v", d.Sections)
	}
	rows := changeRows(t, s, "section")
	if rows < 3 {
		t.Errorf("the change log holds %d section rows, want one per command", rows)
	}

	// A reorder writes the key and nothing else.
	if _, res, err := s.Apply([]Command{
		cmd(t, "s4", "section_reorder", SectionArgs{ID: "x2", OrderKey: ptr("m000005")}),
	}); err != nil || !res[0].OK {
		t.Fatalf("section_reorder: %v %v", err, res)
	}
	secs, _ = s.Sections()
	if secs[0].ID != "x2" {
		t.Errorf("after the reorder the first section is %q, want x2", secs[0].ID)
	}

	// A command with nothing in it is an error, like project_update.
	if _, res, err := s.Apply([]Command{
		cmd(t, "s5", "section_update", SectionArgs{ID: "x1"}),
		cmd(t, "s6", "section_add", SectionArgs{ID: "x9", ProjectID: ptr("p_trip"), Name: ptr("   ")}),
		cmd(t, "s7", "section_reorder", SectionArgs{ID: "x1"}),
	}); err != nil {
		t.Fatal(err)
	} else {
		for i, r := range res {
			if r.OK {
				t.Errorf("the empty command %d was applied", i)
			}
		}
	}
}

// TestSectionMoveTakesItsTasks proves the pair stays in agreement: a section in
// another project would leave its tasks in a column of a project they are not
// in, and no board could draw them.
func TestSectionMoveTakesItsTasks(t *testing.T) {
	s := newStore(t)
	mustApply(t, s, []Command{
		cmd(t, "p1", "project_add", ProjectArgs{ID: "p_a", Name: ptr("A")}),
		cmd(t, "p2", "project_add", ProjectArgs{ID: "p_b", Name: ptr("B")}),
		cmd(t, "s1", "section_add", SectionArgs{ID: "x1", ProjectID: ptr("p_a"), Name: ptr("Errands")}),
		cmd(t, "t1", "task_add", TaskArgs{ID: "t1", ProjectID: ptr("p_a"), SectionID: ptr("x1"), Title: ptr("Buy rope")}),
		cmd(t, "t2", "task_add", TaskArgs{ID: "t2", ProjectID: ptr("p_a"), Title: ptr("Loose end")}),
		cmd(t, "m1", "section_move", SectionArgs{ID: "x1", ProjectID: ptr("p_b")}),
	})
	secs, _ := s.Sections()
	if len(secs) != 1 || secs[0].ProjectID != "p_b" {
		t.Fatalf("the section did not move: %+v", secs)
	}
	moved, err := s.Task("t1")
	if err != nil {
		t.Fatal(err)
	}
	if moved.ProjectID != "p_b" {
		t.Errorf("the task of the section stayed in %q", moved.ProjectID)
	}
	if moved.SectionID == nil || *moved.SectionID != "x1" {
		t.Errorf("the task lost its section: %v", moved.SectionID)
	}
	stayed, err := s.Task("t2")
	if err != nil {
		t.Fatal(err)
	}
	if stayed.ProjectID != "p_a" {
		t.Errorf("a task in no section followed the move, to %q", stayed.ProjectID)
	}

	// A section that does not exist, and a section of another project, are both
	// refused rather than written.
	_, res, err := s.Apply([]Command{
		cmd(t, "bad1", "task_move", MoveArgs{ID: "t2", ProjectID: ptr("p_a"), SectionID: ptr("nope")}),
		cmd(t, "bad2", "task_move", MoveArgs{ID: "t2", ProjectID: ptr("p_a"), SectionID: ptr("x1")}),
		cmd(t, "bad3", "section_move", SectionArgs{ID: "x1", ProjectID: ptr("nope")}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range res {
		if r.OK {
			t.Errorf("the wrong move %d was applied", i)
		}
	}
	if again, _ := s.Task("t2"); again.SectionID != nil {
		t.Errorf("a refused move still wrote a section: %v", again.SectionID)
	}
}

// TestDeletedSectionKeepsItsTasks is the orphan rule. A section is a heading,
// so its removal must never hide or delete work.
func TestDeletedSectionKeepsItsTasks(t *testing.T) {
	s := newStore(t)
	mustApply(t, s, []Command{
		cmd(t, "p1", "project_add", ProjectArgs{ID: "p_a", Name: ptr("A")}),
		cmd(t, "s1", "section_add", SectionArgs{ID: "x1", ProjectID: ptr("p_a"), Name: ptr("Errands")}),
		cmd(t, "t1", "task_add", TaskArgs{ID: "t1", ProjectID: ptr("p_a"), SectionID: ptr("x1"), Title: ptr("Buy rope")}),
		cmd(t, "t2", "task_add", TaskArgs{ID: "t2", ProjectID: ptr("p_a"), SectionID: ptr("x1"), Title: ptr("Buy tape")}),
	})
	before, _ := s.Version()
	mustApply(t, s, []Command{cmd(t, "d1", "section_delete", IDArgs{ID: "x1"})})

	if secs, _ := s.Sections(); len(secs) != 0 {
		t.Fatalf("the section is still live: %+v", secs)
	}
	for _, id := range []string{"t1", "t2"} {
		task, err := s.Task(id)
		if err != nil {
			t.Fatalf("the task %s is gone: %v", id, err)
		}
		if task.DeletedAt != nil {
			t.Errorf("the task %s was deleted with the heading", id)
		}
		if task.ProjectID != "p_a" {
			t.Errorf("the task %s left its project, for %q", id, task.ProjectID)
		}
		if task.SectionID != nil {
			t.Errorf("the task %s still points at a deleted section: %v", id, *task.SectionID)
		}
	}
	// Every task carries its own change_log row, so a client that pulls learns
	// both changes and does not keep drawing an empty column.
	d, err := s.Pull(before)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Sections) != 1 || d.Sections[0].DeletedAt == nil {
		t.Errorf("the pull after the delete = %+v", d.Sections)
	}
	if len(d.Tasks) != 2 {
		t.Errorf("the pull carries %d tasks, want both of them", len(d.Tasks))
	}

	// The undo of the delete: the heading returns, and the client files the
	// tasks back with task_move.
	mustApply(t, s, []Command{
		cmd(t, "r1", "section_restore", IDArgs{ID: "x1"}),
		cmd(t, "r2", "task_move", MoveArgs{ID: "t1", ProjectID: ptr("p_a"), SectionID: ptr("x1")}),
	})
	if secs, _ := s.Sections(); len(secs) != 1 {
		t.Errorf("the section did not come back: %+v", secs)
	}
	if task, _ := s.Task("t1"); task.SectionID == nil || *task.SectionID != "x1" {
		t.Errorf("the task was not filed back")
	}
}

// TestSectionFilterOverRealRows runs the compiled filter against the database,
// because the fixtures in filter/ can only read the SQL. One name is the start
// of another here, which is the case that a LIKE would get wrong.
func TestSectionFilterOverRealRows(t *testing.T) {
	s := newStore(t)
	mustApply(t, s, []Command{
		cmd(t, "p1", "project_add", ProjectArgs{ID: "p_a", Name: ptr("A")}),
		cmd(t, "s1", "section_add", SectionArgs{ID: "x1", ProjectID: ptr("p_a"), Name: ptr("Errand")}),
		cmd(t, "s2", "section_add", SectionArgs{ID: "x2", ProjectID: ptr("p_a"), Name: ptr("Errands in town")}),
		cmd(t, "t1", "task_add", TaskArgs{ID: "t1", ProjectID: ptr("p_a"), SectionID: ptr("x1"), Title: ptr("One")}),
		cmd(t, "t2", "task_add", TaskArgs{ID: "t2", ProjectID: ptr("p_a"), SectionID: ptr("x2"), Title: ptr("Two")}),
		cmd(t, "t3", "task_add", TaskArgs{ID: "t3", ProjectID: ptr("p_a"), Title: ptr("Three")}),
	})
	today := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	cases := map[string][]string{
		"/Errand":          {"t1"},
		"/errand":          {"t1"}, // the match ignores the case
		"/Errands in town": {"t2"},
		"/Errand*":         {"t1", "t2"},
		"no section":       {"t3"},
		"#A & !no section": {"t1", "t2"},
	}
	for query, want := range cases {
		where, args, err := filter.Compile(query, today)
		if err != nil {
			t.Errorf("%q: %v", query, err)
			continue
		}
		tasks, err := s.Query(where, args, 100, 0)
		if err != nil {
			t.Errorf("%q: %v", query, err)
			continue
		}
		got := map[string]bool{}
		for _, task := range tasks {
			got[task.ID] = true
		}
		if len(got) != len(want) {
			t.Errorf("%q matched %d tasks, want %d", query, len(got), len(want))
		}
		for _, id := range want {
			if !got[id] {
				t.Errorf("%q did not match %s", query, id)
			}
		}
	}
}

// mustApply runs a batch and fails on the first refusal.
func mustApply(t *testing.T, s *Store, cmds []Command) {
	t.Helper()
	_, res, err := s.Apply(cmds)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range res {
		if !r.OK {
			t.Fatalf("command %d (%s) failed: %s", i, cmds[i].Type, r.Error)
		}
	}
}

// changeRows counts the change_log rows of one table.
func changeRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM change_log WHERE tbl = ?`, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
