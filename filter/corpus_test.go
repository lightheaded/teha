// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"database/sql"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// The shared filter corpus. parser-fixtures/filter.json holds one account, one
// list of queries and one answer per query. This test is the authority that
// writes the answers: it compiles each query and runs it against real SQLite,
// so an answer is what the server truly returns and not what somebody expected.
//
// internal/webui/assets/filter.test.mjs runs the same file against the web
// evaluator. A term that means two things in two clients fails there.
//
// Regenerate the answers after a deliberate grammar change:
//
//	go test ./filter -run TestFilterCorpus -update
//
// Read the diff before committing it. A test that writes its own expectations
// proves nothing on its own.
var update = flag.Bool("update", false, "write the answers back into the corpus")

type corpusFile struct {
	Comment  string          `json:"comment"`
	Today    string          `json:"today"`
	InboxID  string          `json:"inbox_id"`
	Me       string          `json:"me"`
	Accounts []corpusAccount `json:"accounts"`
	Projects []corpusProject `json:"projects"`
	Sections []corpusSection `json:"sections"`
	Tasks    []corpusTask    `json:"tasks"`
	Comments []corpusComment `json:"comments"`
	Cases    []corpusCase    `json:"cases"`
	Rejects  []corpusBad     `json:"rejects"`
	NoSect   []corpusBad     `json:"no_section_table"`
	NoMade   []corpusBad     `json:"no_created_column"`
	NoWho    []corpusBad     `json:"no_assignee_column"`
	NoTalk   []corpusBad     `json:"no_comment_table"`
}

type corpusAccount struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

type corpusProject struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id,omitempty"`
	Deleted  bool   `json:"deleted,omitempty"`
}

type corpusSection struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}

type corpusTask struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"project_id"`
	SectionID   string   `json:"section_id,omitempty"`
	ParentID    string   `json:"parent_id,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    int      `json:"priority"`
	DueDate     string   `json:"due_date,omitempty"`
	DueTime     string   `json:"due_time,omitempty"`
	RRule       string   `json:"rrule,omitempty"`
	StartDate   string   `json:"start_date,omitempty"`
	Deadline    string   `json:"deadline,omitempty"`
	Assignee    string   `json:"assignee,omitempty"`
	State       string   `json:"state"`
	Labels      []string `json:"labels"`
}

// corpusComment is one line of talk on a task. `comment:` reads the table, so
// the corpus has to hold rows in it.
type corpusComment struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	AccountID string `json:"account_id"`
	Body      string `json:"body"`
	Deleted   bool   `json:"deleted,omitempty"`
}

// Want is nil while no answer is recorded, and an empty list when the answer
// is "no rows". The two must not look alike, or a case that matches nothing
// would pass without ever being written.
type corpusCase struct {
	Q    string   `json:"q"`
	Want []string `json:"want"`
	Note string   `json:"note,omitempty"`
}

// corpusBad is a query with no answer: the grammar must refuse it, or one
// dialect must refuse it because its store keeps no such column.
type corpusBad struct {
	Q    string `json:"q"`
	Note string `json:"note,omitempty"`
}

const corpusPath = "../parser-fixtures/filter.json"

// serverDDL mirrors the tables of internal/store/schema.sql that a filter can
// name. The filter package imports no server code, so the shape is repeated
// here on purpose. Read the two side by side after a schema change.
const serverDDL = `
CREATE TABLE project (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, color TEXT NOT NULL DEFAULT 'grey',
  parent_id TEXT, order_key TEXT NOT NULL, is_inbox INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT,
  version INTEGER NOT NULL);
CREATE TABLE section (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, name TEXT NOT NULL,
  order_key TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  deleted_at TEXT, version INTEGER NOT NULL);
CREATE TABLE label (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, color TEXT NOT NULL DEFAULT 'grey',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT,
  version INTEGER NOT NULL);
CREATE TABLE task (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, section_id TEXT,
  parent_id TEXT, order_key TEXT NOT NULL, title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '', priority INTEGER NOT NULL DEFAULT 4,
  due_date TEXT, due_time TEXT, due_tz TEXT, rrule TEXT,
  rrule_from_completion INTEGER NOT NULL DEFAULT 0, start_date TEXT,
  deadline TEXT, duration_min INTEGER, assignee_id TEXT,
  state TEXT NOT NULL DEFAULT 'open', completed_at TEXT, deleted_at TEXT,
  source_ref TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  version INTEGER NOT NULL);
CREATE TABLE task_label (
  task_id TEXT NOT NULL, label_id TEXT NOT NULL,
  PRIMARY KEY (task_id, label_id));
CREATE TABLE account (
  id TEXT PRIMARY KEY, user_handle BLOB NOT NULL, name TEXT NOT NULL,
  display_name TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE comment (
  id TEXT PRIMARY KEY, task_id TEXT NOT NULL, account_id TEXT NOT NULL,
  body TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  deleted_at TEXT, version INTEGER NOT NULL);
`

func readCorpus(t *testing.T) corpusFile {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(corpusPath))
	if err != nil {
		t.Fatalf("cannot read the corpus: %v", err)
	}
	var c corpusFile
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("cannot read the corpus JSON: %v", err)
	}
	if len(c.Tasks) == 0 || len(c.Cases) == 0 {
		t.Fatal("the corpus has no rows or no cases")
	}
	return c
}

// openCorpusDB builds the server tables in a temporary file and fills them
// from the corpus.
func openCorpusDB(t *testing.T, c corpusFile) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/corpus.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(serverDDL); err != nil {
		t.Fatal(err)
	}
	const now = "2026-08-25T00:00:00Z"
	for _, a := range c.Accounts {
		if _, err := db.Exec(`INSERT INTO account (id, user_handle, name, display_name, created_at)
			VALUES (?,?,?,?,?)`, a.ID, []byte(a.ID), a.Name, a.DisplayName, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range c.Projects {
		var parent, deleted any
		if p.ParentID != "" {
			parent = p.ParentID
		}
		if p.Deleted {
			deleted = now
		}
		if _, err := db.Exec(`INSERT INTO project
			(id,name,color,parent_id,order_key,is_inbox,created_at,updated_at,deleted_at,version)
			VALUES (?,?,'grey',?,'m',?,?,?,?,1)`,
			p.ID, p.Name, parent, boolInt(p.ID == c.InboxID), now, now, deleted); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range c.Sections {
		if _, err := db.Exec(`INSERT INTO section
			(id,project_id,name,order_key,created_at,updated_at,version)
			VALUES (?,?,?,'m',?,?,1)`, s.ID, s.ProjectID, s.Name, now, now); err != nil {
			t.Fatal(err)
		}
	}
	labels := map[string]string{}
	for _, task := range c.Tasks {
		for _, name := range task.Labels {
			if _, seen := labels[name]; seen {
				continue
			}
			id := "l" + strings.ToLower(name)
			labels[name] = id
			if _, err := db.Exec(`INSERT INTO label (id,name,color,created_at,updated_at,version)
				VALUES (?,?,'grey',?,?,1)`, id, name, now, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, task := range c.Tasks {
		if _, err := db.Exec(`INSERT INTO task
			(id,project_id,section_id,parent_id,order_key,title,description,priority,
			 due_date,due_time,rrule,start_date,deadline,assignee_id,state,created_at,updated_at,version)
			VALUES (?,?,?,?,'m',?,?,?,?,?,?,?,?,?,?,?,?,1)`,
			task.ID, task.ProjectID, null(task.SectionID), null(task.ParentID),
			task.Title, task.Description, task.Priority, null(task.DueDate),
			null(task.DueTime), null(task.RRule), null(task.StartDate),
			null(task.Deadline), null(task.Assignee), task.State, now, now); err != nil {
			t.Fatal(err)
		}
		for _, name := range task.Labels {
			if _, err := db.Exec(`INSERT INTO task_label (task_id,label_id) VALUES (?,?)`,
				task.ID, labels[name]); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, cm := range c.Comments {
		var deleted any
		if cm.Deleted {
			deleted = now
		}
		if _, err := db.Exec(`INSERT INTO comment
			(id,task_id,account_id,body,created_at,updated_at,deleted_at,version)
			VALUES (?,?,?,?,?,?,?,1)`,
			cm.ID, cm.TaskID, cm.AccountID, cm.Body, now, now, deleted); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// runQuery compiles one query and returns the ids it selects, in id order so
// that a comparison never depends on the order of a SQL result.
func runQuery(db *sql.DB, q string, today time.Time, me string) ([]string, error) {
	// The account that is asking is part of the question: "assigned to: me"
	// has no answer without it.
	schema := ServerSchema
	schema.Me = me
	where, args, err := CompileFor(q, today, schema)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id FROM task WHERE deleted_at IS NULL AND (`+where+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func TestFilterCorpus(t *testing.T) {
	c := readCorpus(t)
	today, err := time.Parse("2006-01-02", c.Today)
	if err != nil {
		t.Fatalf("bad today in the corpus: %v", err)
	}
	db := openCorpusDB(t, c)

	changed := false
	for i := range c.Cases {
		tc := &c.Cases[i]
		name := tc.Q
		if name == "" {
			name = "(empty)"
		}
		t.Run(name, func(t *testing.T) {
			got, err := runQuery(db, tc.Q, today, c.Me)
			if err != nil {
				t.Fatalf("compile or run: %v", err)
			}
			if *update {
				if !slices.Equal(got, tc.Want) {
					changed = true
				}
				tc.Want = got
				return
			}
			if tc.Want == nil {
				t.Fatalf("the case has no answer. Run: go test ./filter -run TestFilterCorpus -update")
			}
			if !slices.Equal(got, tc.Want) {
				t.Errorf("got %v, want %v", got, tc.Want)
			}
		})
	}

	if *update {
		writeCorpus(t, c)
		if changed {
			t.Log("the corpus answers changed. Read the diff before you commit it.")
		}
		return
	}

	// A query the grammar must refuse. A refusal is a promise too: a client
	// shows the sentence instead of quietly listing the wrong rows.
	for _, tc := range c.Rejects {
		t.Run("rejects "+tc.Q, func(t *testing.T) {
			schema := ServerSchema
			schema.Me = c.Me
			if _, _, err := CompileFor(tc.Q, today, schema); err == nil {
				t.Errorf("the grammar accepted %q, and it must not", tc.Q)
			}
		})
	}

	// Each list names one thing a store can lack. A client that lacks it must
	// fail with a sentence rather than name a column it has not got.
	//
	// The dialect is built here, by taking the capability away from the server
	// names, and not by naming a client. A client gains a table as it grows,
	// and the promise is about the missing table and not about the phone: the
	// phone kept no section and no assignee until it joined the household.
	// internal/webui/assets/filter.test.mjs does the same with its caps.
	lists := []struct {
		lack  string
		cases []corpusBad
		strip func(*Schema)
	}{
		{"a section table", c.NoSect, func(s *Schema) { s.Section = ""; s.SectionID = "" }},
		{"a creation date", c.NoMade, func(s *Schema) { s.CreatedAt = "" }},
		{"an assignee column", c.NoWho, func(s *Schema) { s.Assignee = ""; s.Account = "" }},
		{"a comment table", c.NoTalk, func(s *Schema) { s.Comment = "" }},
	}
	for _, list := range lists {
		schema := ServerSchema
		schema.Me = c.Me
		list.strip(&schema)
		for _, tc := range list.cases {
			t.Run("without "+list.lack+": "+tc.Q, func(t *testing.T) {
				if _, _, err := CompileFor(tc.Q, today, schema); err == nil {
					t.Errorf("%q was accepted by a store with no %s", tc.Q, list.lack)
				}
			})
		}
	}
}

// writeCorpus saves the corpus with the answers filled in, one row per line.
// The standard indenting writer puts every field of every row on its own line,
// which turns a hundred readable cases into a thousand lines that no reviewer
// reads. A row on one line is a diff a person can check.
func writeCorpus(t *testing.T, c corpusFile) {
	t.Helper()
	var b strings.Builder
	b.WriteString("{\n")
	writeField(t, &b, "comment", c.Comment, true)
	writeField(t, &b, "today", c.Today, true)
	writeField(t, &b, "inbox_id", c.InboxID, true)
	writeField(t, &b, "me", c.Me, true)
	writeRows(t, &b, "accounts", c.Accounts)
	writeRows(t, &b, "projects", c.Projects)
	writeRows(t, &b, "sections", c.Sections)
	writeRows(t, &b, "tasks", c.Tasks)
	writeRows(t, &b, "comments", c.Comments)
	writeRows(t, &b, "cases", c.Cases)
	writeRows(t, &b, "rejects", c.Rejects)
	writeRows(t, &b, "no_section_table", c.NoSect)
	writeRows(t, &b, "no_created_column", c.NoMade)
	writeRows(t, &b, "no_assignee_column", c.NoWho)
	writeRows(t, &b, "no_comment_table", c.NoTalk)
	out := strings.TrimSuffix(b.String(), ",\n") + "\n}\n"
	if err := os.WriteFile(filepath.FromSlash(corpusPath), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

// compact writes one value as JSON on one line, with no HTML escaping. The
// standard encoder writes an ampersand as \u0026, and the corpus is full of
// them: "&" is the AND operator of the grammar.
func compact(t *testing.T, value any) string {
	t.Helper()
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func writeField(t *testing.T, b *strings.Builder, name string, value any, comma bool) {
	t.Helper()
	raw := compact(t, value)
	b.WriteString("  \"" + name + "\": " + raw)
	if comma {
		b.WriteString(",")
	}
	b.WriteString("\n")
}

func writeRows[T any](t *testing.T, b *strings.Builder, name string, rows []T) {
	t.Helper()
	b.WriteString("  \"" + name + "\": [\n")
	for i, row := range rows {
		b.WriteString("    " + compact(t, row))
		if i < len(rows)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ],\n")
}
