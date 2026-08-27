// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"database/sql"
	"sort"
	"strings"
	"testing"

	// The driver is a test dependency only. The filter package itself opens no
	// database. It is here because a compiled WHERE clause is worth nothing
	// until a real SQLite has run it: a name that does not exist, or a LIKE
	// pattern that escapes wrongly, both compile to a string that looks right.
	_ "modernc.org/sqlite"
)

// roomDDL mirrors android/app/src/main/kotlin/io/github/lightheaded/teha/
// data/db/Entities.kt, which is what Room creates on the phone. Read the two
// side by side after any change to either one.
const roomDDL = `
CREATE TABLE tasks (
  id TEXT PRIMARY KEY NOT NULL, projectId TEXT NOT NULL, parentId TEXT,
  orderKey TEXT NOT NULL, title TEXT NOT NULL, description TEXT NOT NULL,
  priority INTEGER NOT NULL, dueDate TEXT, dueTime TEXT, dueTz TEXT,
  rrule TEXT, rruleFromCompletion INTEGER NOT NULL, startDate TEXT,
  deadline TEXT, durationMin INTEGER, state TEXT NOT NULL, completedAt TEXT,
  deletedAt TEXT, labels TEXT NOT NULL, version INTEGER NOT NULL);
CREATE TABLE projects (
  id TEXT PRIMARY KEY NOT NULL, name TEXT NOT NULL, color TEXT NOT NULL,
  parentId TEXT, orderKey TEXT NOT NULL, isInbox INTEGER NOT NULL,
  deletedAt TEXT, version INTEGER NOT NULL);
CREATE TABLE labels (
  id TEXT PRIMARY KEY NOT NULL, name TEXT NOT NULL, color TEXT NOT NULL,
  deletedAt TEXT, version INTEGER NOT NULL);
`

// roomRow is one task in the fixture below. labels holds what the Room type
// converter writes: every label name of the task, joined by a comma.
type roomRow struct {
	id        string
	projectID string
	parentID  string
	title     string
	notes     string
	priority  int
	dueDate   string
	dueTime   string
	rrule     string
	startDate string
	deadline  string
	state     string
	labels    string
}

// The fixture. today is 2026-08-25, a Tuesday.
var roomRows = []roomRow{
	{id: "t1", projectID: "home", title: "Fix the sink", priority: 3, dueDate: "2026-08-24", state: "open", labels: "work"},
	{id: "t2", projectID: "home", title: "Do homework", priority: 1, dueDate: "2026-08-25", dueTime: "09:30", state: "open", labels: "homework"},
	{id: "t3", projectID: "inbox", title: "Buy milk", priority: 4, state: "open", labels: ""},
	{id: "t4", projectID: "shed", title: "Read the manual", priority: 4, dueDate: "2026-08-28", state: "open", labels: "50%,work"},
	{id: "t5", projectID: "home", title: "Pay the bill", priority: 2, dueDate: "2026-08-26", deadline: "2026-08-28", state: "open", labels: "a_b"},
	{id: "t6", projectID: "home", title: "Wash the car", priority: 4, dueDate: "2026-08-24", state: "done", labels: "work"},
	// A label name that holds a comma. The joined column cannot tell it from
	// two labels, and the grammar cannot name it either, because a comma is
	// the OR operator. The row is here to prove that it fools no other match.
	{id: "t7", projectID: "shed", title: "Sort the shelf", priority: 4, state: "open", labels: "home, work"},
	{id: "t8", projectID: "shed", title: "Oil the hinge", priority: 4, state: "open", labels: "axb"},
	{id: "t9", projectID: "home", parentID: "t1", title: "Buy a washer", priority: 4, state: "open", labels: ""},
	{id: "t10", projectID: "home", title: "Water the plants", priority: 4, dueDate: "2026-08-27", rrule: "FREQ=WEEKLY", startDate: "2026-09-10", state: "open", labels: ""},
	{id: "t11", projectID: "gone", title: "In a deleted project", priority: 4, state: "open", labels: ""},
}

// openRoomDB builds the Room tables in a temporary file and fills them.
func openRoomDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/room.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(roomDDL); err != nil {
		t.Fatal(err)
	}
	null := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	for _, r := range roomRows {
		_, err := db.Exec(`INSERT INTO tasks (id, projectId, parentId, orderKey, title,
			description, priority, dueDate, dueTime, dueTz, rrule, rruleFromCompletion,
			startDate, deadline, durationMin, state, completedAt, deletedAt, labels, version)
			VALUES (?,?,?,'m',?,?,?,?,?,NULL,?,0,?,?,NULL,?,NULL,NULL,?,1)`,
			r.id, r.projectID, null(r.parentID), r.title, r.notes, r.priority,
			null(r.dueDate), null(r.dueTime), null(r.rrule), null(r.startDate),
			null(r.deadline), r.state, r.labels)
		if err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
	}
	projects := []struct {
		id, name, parent string
		inbox            int
		deleted          string
	}{
		{id: "inbox", name: "Inbox", inbox: 1},
		{id: "home", name: "Home"},
		{id: "shed", name: "Shed", parent: "home"},
		{id: "gone", name: "Gone", deleted: "2026-08-01T00:00:00Z"},
	}
	for _, p := range projects {
		_, err := db.Exec(`INSERT INTO projects (id, name, color, parentId, orderKey,
			isInbox, deletedAt, version) VALUES (?,?,'grey',?,'m',?,?,1)`,
			p.id, p.name, null(p.parent), p.inbox, null(p.deleted))
		if err != nil {
			t.Fatalf("insert project %s: %v", p.id, err)
		}
	}
	return db
}

// roomIDs runs one query in the Room dialect and returns the task ids it found.
//
// The wrapper repeats what TehaRepository.tasks builds on the phone: the
// compiled filter is a WHERE clause only, so the caller adds the deleted-row
// test and the sort.
func roomIDs(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	where, args, err := CompileFor(query, today, RoomSchema)
	if err != nil {
		t.Fatalf("%q: %v", query, err)
	}
	rows, err := db.Query(`SELECT id FROM tasks WHERE deletedAt IS NULL AND (`+where+`)
		ORDER BY (dueDate IS NULL), dueDate ASC, priority ASC, orderKey ASC`, args...)
	if err != nil {
		t.Fatalf("%q: %v\nSQL: %s", query, err, where)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%q: %v", query, err)
	}
	return out
}

// TestRoomDialectRuns is the proof of the shim. Every case runs against a real
// SQLite database that carries the Room names.
//
// A case compares the rows as a set. TestRoomViewOrder covers the order, where
// the sort keys are all different and the answer is therefore stable.
func TestRoomDialectRuns(t *testing.T) {
	db := openRoomDB(t)
	cases := []struct {
		query string
		want  []string
	}{
		// The six views the phone shows.
		{"today", []string{"t1", "t2"}},
		{"overdue", []string{"t1"}},
		{"week", []string{"t1", "t2", "t4", "t5", "t10"}},
		{"#inbox", []string{"t3"}},
		{"no date", []string{"t3", "t7", "t8", "t9", "t11"}},
		{"p1", []string{"t2"}},

		// Dates.
		{"tomorrow", []string{"t5"}},
		{"yesterday", []string{"t1"}},
		{"no time", []string{"t1", "t3", "t4", "t5", "t7", "t8", "t9", "t10", "t11"}},
		{"has time", []string{"t2"}},
		{"before: today", []string{"t1"}},
		{"after: today", []string{"t4", "t5", "t10"}},
		{"date: 26.08.2026", []string{"t5"}},
		{"deadline", []string{"t5"}},
		{"no deadline", []string{"t1", "t2", "t3", "t4", "t7", "t8", "t9", "t10", "t11"}},
		{"deadline: friday", []string{"t5"}},
		{"started", []string{"t1", "t2", "t3", "t4", "t5", "t7", "t8", "t9", "t11"}},
		{"deferred", []string{"t10"}},

		// Shape and state.
		{"p2", []string{"t5"}},
		{"no priority", []string{"t3", "t4", "t7", "t8", "t9", "t10", "t11"}},
		{"recurring", []string{"t10"}},
		{"subtask", []string{"t9"}},
		{"no parent", []string{"t1", "t2", "t3", "t4", "t5", "t7", "t8", "t10", "t11"}},
		{"done", []string{"t6"}},
		{"open & p1", []string{"t2"}},

		// Projects. A deleted project answers nothing, and #Shed does not
		// carry the parent, while ##Home carries the child.
		{"#Home", []string{"t1", "t2", "t5", "t9", "t10"}},
		{"#home", []string{"t1", "t2", "t5", "t9", "t10"}},
		{"#Shed", []string{"t4", "t7", "t8"}},
		{"#Sh*", []string{"t4", "t7", "t8"}},
		{"##Home", []string{"t1", "t2", "t4", "t5", "t7", "t8", "t9", "t10"}},
		{"#Gone", nil},

		// Labels, over the comma-joined column.
		//
		// %work must not match "homework", and it must not match the stored
		// name "home, work" either, because that name carries a space.
		{"%work", []string{"t1", "t4"}},
		{"@work", []string{"t1", "t4"}},
		{"%WORK", []string{"t1", "t4"}},
		{"%homework", []string{"t2"}},
		{"%home*", []string{"t2", "t7"}},
		{"no labels", []string{"t3", "t9", "t10", "t11"}},
		// A name that holds a LIKE wildcard. Without ESCAPE these two answer
		// every labelled task.
		{"%50%", []string{"t4"}},
		{"%a_b", []string{"t5"}},

		// Search. The Room database holds no fts5 table, so this is a LIKE
		// over the title and the description.
		{"search: milk", []string{"t3"}},
		{"search: MILK", []string{"t3"}},
		{"washer", []string{"t9"}},
		// A search term that names a column proves the mapping is not a
		// rewrite of the finished SQL. A rewrite would turn the term itself
		// into a Room column name.
		{"search: dueDate", nil},
		{"search: due_date", nil},

		// The operators.
		{"overdue | tomorrow", []string{"t1", "t5"}},
		{"#Home & p1", []string{"t2"}},
		{"#Home, #Shed", []string{"t1", "t2", "t4", "t5", "t7", "t8", "t9", "t10"}},
		{"!%work & #Home", []string{"t2", "t5", "t9", "t10"}},
		{"(today | tomorrow) & !p1", []string{"t1", "t5"}},
	}
	for _, c := range cases {
		got := roomIDs(t, db, c.query)
		sort.Strings(got)
		want := append([]string(nil), c.want...)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%q found %v, want %v", c.query, got, want)
		}
	}
}

// The order of a view belongs to the query around the filter, not to the
// filter. Today therefore puts the overdue rows on top, and a row with no date
// sorts last.
func TestRoomViewOrder(t *testing.T) {
	db := openRoomDB(t)
	// t1 is 24 August, t2 is the 25th, t5 the 26th, t10 the 27th, t4 the 28th.
	got := roomIDs(t, db, "week")
	want := []string{"t1", "t2", "t5", "t10", "t4"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("week is ordered %v, want %v", got, want)
	}
	got = roomIDs(t, db, "today")
	want = []string{"t1", "t2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("today is ordered %v, want %v, so the overdue row is not on top", got, want)
	}
}

// The compiled Room SQL must never carry a server name. A single one of them
// makes the phone throw at run time, where no Go test can see it.
func TestRoomSQLNamesNoServerColumn(t *testing.T) {
	queries := []string{
		"today", "tomorrow", "yesterday", "overdue", "no date", "no time",
		"has time", "recurring", "subtask", "no parent", "no priority",
		"no deadline", "deadline", "no labels", "started", "deferred", "done",
		"wont do", "open", "any state", "week", "p1", "p4", "#Home", "#inbox",
		"#Home*", "##Home", "%work", "@work", "%work*", "search: text",
		"date: today", "before: today", "after: today", "deadline: today",
		"deadline before: today", "a bare word",
	}
	banned := []string{
		"due_date", "due_time", "project_id", "parent_id", "start_date",
		"deleted_at", "created_at", "task_label", "label_id", "task_id",
		"task_fts", "FROM task ", "FROM project ", "FROM label ",
	}
	for _, q := range queries {
		sql, _, err := CompileFor(q, today, RoomSchema)
		if err != nil {
			t.Errorf("%q: %v", q, err)
			continue
		}
		for _, name := range banned {
			if strings.Contains(sql, name) {
				t.Errorf("%q compiled to %q, which names %q", q, sql, name)
			}
		}
	}
}

// A term that needs a column the client does not keep must fail with a
// sentence, and never compile to SQL that names the absent column.
// The phone holds no section table, so a section term must fail with a sentence
// rather than compile to SQL that names a table Room never declared.
func TestRoomRefusesSectionTerm(t *testing.T) {
	for _, q := range []string{"/Winter", "/Win*", "no section", "no sections"} {
		sql, _, err := CompileFor(q, today, RoomSchema)
		if err == nil {
			t.Errorf("%q compiled to %q, want a failure", q, sql)
			continue
		}
		if !strings.Contains(err.Error(), "section") {
			t.Errorf("%q failed with %q, which does not name the term", q, err)
		}
	}
	// The server keeps the table, so the same terms work there.
	for _, q := range []string{"/Winter", "/Win*", "no section"} {
		if _, _, err := Compile(q, today); err != nil {
			t.Errorf("the server dialect refused %q with %v", q, err)
		}
	}
}

func TestRoomRefusesCreatedTerm(t *testing.T) {
	for _, q := range []string{"created: today", "created before: today", "created after: today"} {
		sql, _, err := CompileFor(q, today, RoomSchema)
		if err == nil {
			t.Errorf("%q compiled to %q, want a failure", q, sql)
			continue
		}
		if !strings.Contains(err.Error(), "created") {
			t.Errorf("%q failed with %q, which does not name the term", q, err)
		}
	}
	// The server keeps the column, so the same term works there.
	if _, _, err := Compile("created: today", today); err != nil {
		t.Errorf("the server dialect refused created: with %v", err)
	}
}

// The server dialect must not move. Every other client and the MCP tools read
// it, so a change to the naming scheme has to leave it exactly as it was.
func TestServerDialectIsTheDefault(t *testing.T) {
	for _, q := range []string{"", "today", "%work", "##Home", "#inbox", "created: today"} {
		wantSQL, wantArgs, wantErr := CompileFor(q, today, ServerSchema)
		gotSQL, gotArgs, gotErr := Compile(q, today)
		if gotSQL != wantSQL || len(gotArgs) != len(wantArgs) || (gotErr == nil) != (wantErr == nil) {
			t.Errorf("%q: Compile and CompileFor with ServerSchema disagree", q)
		}
	}
	sql, _, err := Compile("%work", today)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "task_label") {
		t.Errorf("the server dialect stopped joining task_label: %s", sql)
	}
}

// escapeLike is what keeps a label named "50%" from matching every label.
func TestEscapeLike(t *testing.T) {
	cases := map[string]string{
		"work": "work",
		"50%":  `50\%`,
		"a_b":  `a\_b`,
		`a\b`:  `a\\b`,
		`%_\`:  `\%\_\\`,
	}
	for in, want := range cases {
		if got := escapeLike(in); got != want {
			t.Errorf("escapeLike(%q) is %q, want %q", in, got, want)
		}
	}
}

// A comma is the OR operator, so no dialect can name a label that holds one.
// The test records the behaviour rather than a wish: the query becomes two
// terms, and the stored name fools neither of them.
func TestCommaInALabelNameIsTwoTerms(t *testing.T) {
	db := openRoomDB(t)
	got := roomIDs(t, db, "%home, work")
	sort.Strings(got)
	// The first term reads the label "home", which the stored name "home,
	// work" answers, because the pad and the join use the same separator. The
	// second term is a bare word, so it searches the title: "Do homework".
	want := []string{"t2", "t7"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf(`"%%home, work" found %v, want %v`, got, want)
	}
}
