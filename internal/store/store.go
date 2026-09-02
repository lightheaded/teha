// SPDX-License-Identifier: AGPL-3.0-or-later

// Package store holds the SQLite database and every write path.
//
// One file holds one account. The store is the only writer, so it serializes
// every transaction behind a mutex. Each write bumps a monotonic version in
// change_log, and a client pulls with that version.
package store

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// InboxID is the fixed id of the inbox project, so a client can address it
// before its first sync.
const InboxID = "inbox"

// Store owns the database handle.
type Store struct {
	db *sql.DB
	mu sync.Mutex // one writer at a time
	// Now returns the current time. Tests replace it.
	Now func() time.Time
}

// Open opens or creates the database at path and applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite plus one writer goroutine
	s := &Store{db: db, Now: time.Now}
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := s.seedInbox(); err != nil {
		return nil, err
	}
	if err := s.seedOwner(); err != nil {
		return nil, err
	}
	return s, nil
}

// migrate adds a column that a database from an older build does not have.
//
// schema.sql runs with CREATE TABLE IF NOT EXISTS, which does nothing at all to
// a table that already exists. A new column therefore never reaches a live
// account from that file, and the account keeps working until the first query
// names the column. An ALTER is the only way in, and SQLite has no
// "ADD COLUMN IF NOT EXISTS", so each step asks PRAGMA table_info first.
//
// One code path serves a fresh file and an old one: the column is absent from
// schema.sql, so both files take the ALTER. The two cannot drift, and there is
// no version number to keep in step. An index on the new column belongs here as
// well, because schema.sql runs before the ALTER.
//
// The data is safe. ADD COLUMN rewrites no row: SQLite records the new column
// in the header and reads a missing value as the default, which is NULL here.
// A task in an upgraded account is therefore in no section, which is what it
// was before the column existed. See DECISIONS.md D-012.
func (s *Store) migrate() error {
	steps := []struct{ table, column, ddl string }{
		{"task", "section_id", `ALTER TABLE task ADD COLUMN section_id TEXT REFERENCES section(id)`},
		// The household. See internal/store/account.go.
		{"account", "token_hash", `ALTER TABLE account ADD COLUMN token_hash TEXT NOT NULL DEFAULT ''`},
		{"account", "inbox_id", `ALTER TABLE account ADD COLUMN inbox_id TEXT`},
		{"account", "is_owner", `ALTER TABLE account ADD COLUMN is_owner INTEGER NOT NULL DEFAULT 0`},
		{"project", "owner_id", `ALTER TABLE project ADD COLUMN owner_id TEXT`},
		// A reminder is personal, even on a shared task: two people do not
		// want one nudge between them.
		{"reminder", "account_id", `ALTER TABLE reminder ADD COLUMN account_id TEXT`},
		// A push subscription is one browser of one person. Without the
		// column, a second account's browser would receive the first
		// account's reminders.
		{"push_subscription", "account_id", `ALTER TABLE push_subscription ADD COLUMN account_id TEXT`},
	}
	for _, st := range steps {
		has, err := s.hasColumn(st.table, st.column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := s.db.Exec(st.ddl); err != nil {
			return fmt.Errorf("%s.%s: %w", st.table, st.column, err)
		}
	}
	// The board reads one project at a time, and Pull reads by version, so the
	// index that matches is the pair.
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS task_by_section ON task(section_id)`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS project_by_owner ON project(owner_id)`); err != nil {
		return err
	}

	// Fill in what the new columns mean for a file that predates them. Every
	// row of such a file belongs to the owner, because there was nobody else.
	// Each statement names IS NULL, so it does nothing on the second run and
	// nothing to a row that a later account wrote.
	backfill := []string{
		`UPDATE project SET owner_id = '` + OwnerID + `' WHERE owner_id IS NULL`,
		`UPDATE reminder SET account_id = '` + OwnerID + `' WHERE account_id IS NULL`,
		`UPDATE push_subscription SET account_id = '` + OwnerID + `' WHERE account_id IS NULL`,
		`UPDATE account SET inbox_id = '` + InboxID + `', is_owner = 1 WHERE id = '` + OwnerID + `'`,
		`UPDATE account SET inbox_id = 'inbox_' || id WHERE inbox_id IS NULL`,
	}
	for _, q := range backfill {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("backfill: %w", err)
		}
	}
	return nil
}

func (s *Store) hasColumn(table, column string) (bool, error) {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Checkpoint moves what the write-ahead log holds into the database file.
//
// This exists for the backup, and the reason is worth the paragraph. The
// server holds one long-lived connection, so SQLite writes into the WAL and
// checkpoints it only when it grows past its own threshold, about four
// megabytes. Litestream replicates from the database file, so between two
// checkpoints a replica can be minutes or days behind what the person typed,
// and a restore then loses exactly that work.
//
// scripts/restore-drill.sh is the proof: with no checkpoint the drill restores
// a file that has the seed and none of the writes after it.
//
// PASSIVE is the right mode. It copies what it can and gives up at once when a
// reader is in the way, so a checkpoint never blocks the person typing. The
// next tick tries again.
func (s *Store) Checkpoint() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`)
	return err
}

func (s *Store) seedInbox() error {
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM project WHERE id = ?`, InboxID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	now := s.stamp()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	v, err := bump(tx, "project", InboxID, "insert", now)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO project (id, name, color, parent_id, order_key, is_inbox,
		owner_id, created_at, updated_at, version) VALUES (?,?,?,NULL,?,1,?,?,?,?)`,
		InboxID, "Inbox", "grey", "m", OwnerID, now, now, v)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) stamp() string { return s.Now().UTC().Format(time.RFC3339) }

// Version returns the current account version.
func (s *Store) Version() (int64, error) {
	var v sql.NullInt64
	err := s.db.QueryRow(`SELECT max(version) FROM change_log`).Scan(&v)
	if err != nil {
		return 0, err
	}
	return v.Int64, nil
}

// bump writes a change_log row and returns the new version.
func bump(tx *sql.Tx, table, id, op, at string) (int64, error) {
	res, err := tx.Exec(`INSERT INTO change_log (tbl, row_id, op, at) VALUES (?,?,?,?)`, table, id, op, at)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// --- rows -------------------------------------------------------------------

// Project is a list of tasks. Projects nest.
type Project struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Color     string  `json:"color"`
	ParentID  *string `json:"parent_id,omitempty"`
	OrderKey  string  `json:"order_key"`
	IsInbox   bool    `json:"is_inbox"`
	DeletedAt *string `json:"deleted_at,omitempty"`
	Version   int64   `json:"v"`
}

// Section is a heading inside one project, and a column on the board layout.
type Section struct {
	ID        string  `json:"id"`
	ProjectID string  `json:"project_id"`
	Name      string  `json:"name"`
	OrderKey  string  `json:"order_key"`
	DeletedAt *string `json:"deleted_at,omitempty"`
	Version   int64   `json:"v"`
}

// Label is a tag on a task.
type Label struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Color     string  `json:"color"`
	DeletedAt *string `json:"deleted_at,omitempty"`
	Version   int64   `json:"v"`
}

// Comment is one line of talk on a task.
//
// AccountID names the author, and the author is the only person who may change
// the line. Two people share a list, so a comment with no author reads as if
// the household said it.
type Comment struct {
	ID        string  `json:"id"`
	TaskID    string  `json:"task_id"`
	AccountID string  `json:"account_id"`
	Body      string  `json:"body"`
	CreatedAt string  `json:"created_at"`
	DeletedAt *string `json:"deleted_at,omitempty"`
	Version   int64   `json:"v"`
}

const commentCols = `id, task_id, account_id, body, created_at, deleted_at, version`

func scanComment(rows *sql.Rows) (Comment, error) {
	var c Comment
	err := rows.Scan(&c.ID, &c.TaskID, &c.AccountID, &c.Body, &c.CreatedAt, &c.DeletedAt, &c.Version)
	return c, err
}

// Task is one item of work.
type Task struct {
	ID           string  `json:"id"`
	ProjectID    string  `json:"project_id"`
	SectionID    *string `json:"section_id,omitempty"`
	ParentID     *string `json:"parent_id,omitempty"`
	OrderKey     string  `json:"order_key"`
	Title        string  `json:"title"`
	Description  string  `json:"description,omitempty"`
	Priority     int     `json:"priority"`
	DueDate      *string `json:"due_date,omitempty"`
	DueTime      *string `json:"due_time,omitempty"`
	DueTz        *string `json:"due_tz,omitempty"`
	RRule        *string `json:"rrule,omitempty"`
	FromComplete bool    `json:"rrule_from_completion,omitempty"`
	StartDate    *string `json:"start_date,omitempty"`
	Deadline     *string `json:"deadline,omitempty"`
	DurationMin  *int    `json:"duration_min,omitempty"`
	// AssigneeID names who does this task. It is only meaningful in a shared
	// project, and it is empty everywhere else.
	AssigneeID  *string  `json:"assignee_id,omitempty"`
	State       string   `json:"state"`
	CompletedAt *string  `json:"completed_at,omitempty"`
	DeletedAt   *string  `json:"deleted_at,omitempty"`
	SourceRef   *string  `json:"source_ref,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Version     int64    `json:"v"`
}

const taskCols = `id, project_id, section_id, parent_id, order_key, title, description, priority,
	due_date, due_time, due_tz, rrule, rrule_from_completion, start_date, deadline, duration_min,
	assignee_id, state, completed_at, deleted_at, source_ref, version`

func scanTask(rows *sql.Rows) (Task, error) {
	var t Task
	var fromComplete int
	err := rows.Scan(&t.ID, &t.ProjectID, &t.SectionID, &t.ParentID, &t.OrderKey, &t.Title, &t.Description,
		&t.Priority, &t.DueDate, &t.DueTime, &t.DueTz, &t.RRule, &fromComplete, &t.StartDate, &t.Deadline,
		&t.DurationMin, &t.AssigneeID, &t.State, &t.CompletedAt, &t.DeletedAt, &t.SourceRef, &t.Version)
	t.FromComplete = fromComplete == 1
	return t, err
}

// --- reads ------------------------------------------------------------------

// Delta is everything that changed after a version.
type Delta struct {
	Version   int64      `json:"version"`
	Projects  []Project  `json:"projects"`
	Sections  []Section  `json:"sections"`
	Labels    []Label    `json:"labels"`
	Tasks     []Task     `json:"tasks"`
	Reminders []Reminder `json:"reminders"`
	Comments  []Comment  `json:"comments"`
	// Reset tells a client to throw away what it holds and pull from zero.
	// A scoped pull cannot report that a row went away: when a project stops
	// being shared, the delta simply stops carrying it, and the client would
	// keep a copy for ever. The server therefore says so once. See
	// withMembershipChange in account.go.
	Reset bool `json:"reset,omitempty"`
	// Inbox names the project that a capture with no project belongs in. Each
	// account has its own, so a client must not assume the fixed id.
	Inbox string `json:"inbox,omitempty"`
}

// Pull returns every row of the owner with a version above since. It is what a
// file with one account has always answered.
func (s *Store) Pull(since int64) (Delta, error) {
	return s.PullFor(since, OwnerID)
}

// PullFor returns every row above since that one account may see.
//
// The version counter stays global. A client's since is a watermark, and a row
// it may not see is skipped, so two accounts share one ordering and neither
// needs a counter of its own. The cost is that a client can receive a delta
// with no rows in it, which is exactly what a quiet period looks like anyway.
func (s *Store) PullFor(since int64, accountID string) (Delta, error) {
	d := Delta{Projects: []Project{}, Sections: []Section{}, Labels: []Label{}, Tasks: []Task{},
		Reminders: []Reminder{}, Comments: []Comment{}}
	v, err := s.Version()
	if err != nil {
		return d, err
	}
	d.Version = v

	moved, err := s.membershipMoved(accountID, since)
	if err != nil {
		return d, err
	}
	if moved && since > 0 {
		// Start again. The client asked from a version before its view
		// changed, so a delta cannot describe the difference.
		d.Reset = true
		since = 0
	}
	if a, err := s.AccountByID(accountID); err == nil {
		d.Inbox = a.InboxID
	}

	// mine is the sub-select of the projects this account may see. Every other
	// query in this function hangs off it, because every row belongs to a
	// project.
	const mine = `(SELECT id FROM project WHERE owner_id = ?
		UNION SELECT project_id FROM project_member WHERE account_id = ?)`

	rows, err := s.db.Query(`SELECT id, name, color, parent_id, order_key, is_inbox, deleted_at, version
		FROM project WHERE version > ? AND id IN `+mine+` ORDER BY version`, since, accountID, accountID)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var p Project
		var inbox int
		if err := rows.Scan(&p.ID, &p.Name, &p.Color, &p.ParentID, &p.OrderKey, &inbox, &p.DeletedAt, &p.Version); err != nil {
			rows.Close()
			return d, err
		}
		p.IsInbox = inbox == 1
		d.Projects = append(d.Projects, p)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT id, project_id, name, order_key, deleted_at, version
		FROM section WHERE version > ? AND project_id IN `+mine+` ORDER BY version`, since, accountID, accountID)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var sec Section
		if err := rows.Scan(&sec.ID, &sec.ProjectID, &sec.Name, &sec.OrderKey, &sec.DeletedAt, &sec.Version); err != nil {
			rows.Close()
			return d, err
		}
		d.Sections = append(d.Sections, sec)
	}
	rows.Close()

	// A label is household vocabulary and it is not scoped. A name only ever
	// shows on a task, so a person still sees a label of theirs on a task of
	// theirs and nothing else. docs/BACKLOG.md records the limit.
	rows, err = s.db.Query(`SELECT id, name, color, deleted_at, version FROM label WHERE version > ? ORDER BY version`, since)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Color, &l.DeletedAt, &l.Version); err != nil {
			rows.Close()
			return d, err
		}
		d.Labels = append(d.Labels, l)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT `+taskCols+` FROM task WHERE version > ? AND project_id IN `+mine+
		` ORDER BY version`, since, accountID, accountID)
	if err != nil {
		return d, err
	}
	var ids []string
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return d, err
		}
		d.Tasks = append(d.Tasks, t)
		ids = append(ids, t.ID)
	}
	rows.Close()

	// A comment hangs off a task, which hangs off a project, so the same
	// visibility clause answers for it. A comment of a task the account cannot
	// see never leaves the file.
	rows, err = s.db.Query(`SELECT `+commentCols+` FROM comment
		WHERE version > ? AND task_id IN (SELECT id FROM task WHERE project_id IN `+mine+`)
		ORDER BY version`, since, accountID, accountID)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			rows.Close()
			return d, err
		}
		d.Comments = append(d.Comments, c)
	}
	rows.Close()

	// A reminder is personal, even on a shared task. Two people who share a
	// chore do not want one nudge between them, and neither wants to see the
	// other's.
	rows, err = s.db.Query(`SELECT `+reminderCols+` FROM reminder
		WHERE version > ? AND coalesce(account_id, ?) = ? ORDER BY version`, since, OwnerID, accountID)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			rows.Close()
			return d, err
		}
		d.Reminders = append(d.Reminders, r)
	}
	rows.Close()

	if len(ids) > 0 {
		byTask, err := s.labelsFor(ids)
		if err != nil {
			return d, err
		}
		for i := range d.Tasks {
			d.Tasks[i].Labels = byTask[d.Tasks[i].ID]
		}
	}
	return d, nil
}

func (s *Store) labelsFor(taskIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(taskIDs) == 0 {
		return out, nil
	}
	q := `SELECT tl.task_id, l.name FROM task_label tl JOIN label l ON l.id = tl.label_id
		WHERE tl.task_id IN (` + placeholders(len(taskIDs)) + `) AND l.deleted_at IS NULL ORDER BY l.name`
	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		args[i] = id
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var tid, name string
		if err := rows.Scan(&tid, &name); err != nil {
			return out, err
		}
		out[tid] = append(out[tid], name)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// Query runs a compiled filter and returns matching tasks, newest order first.
func (s *Store) Query(where string, args []any, limit int, offset int) ([]Task, error) {
	return s.QueryFor(where, args, limit, offset, "")
}

// scopeFor builds the visibility clause for one account, over the column that
// names the project. An empty account reads the whole file.
func scopeFor(accountID, column string) (string, []any) {
	if accountID == "" {
		return "", nil
	}
	return ` AND ` + column + ` IN ` + mineSQL, []any{accountID, accountID}
}

// mineSQL is the sub-select of the projects one account may see. It takes the
// account id twice: once for what they own, once for what they were given.
const mineSQL = `(SELECT id FROM project WHERE owner_id = ?
	UNION SELECT project_id FROM project_member WHERE account_id = ?)`

// QueryFor runs a compiled filter for one account. An empty account reads the
// whole file, which is what a one-account tool wants.
func (s *Store) QueryFor(where string, args []any, limit, offset int, accountID string) ([]Task, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	scope := ""
	all := append([]any{}, args...)
	if accountID != "" {
		scope = ` AND project_id IN ` + mineSQL
		all = append(all, accountID, accountID)
	}
	q := `SELECT ` + taskCols + ` FROM task WHERE deleted_at IS NULL AND (` + where + `)` + scope + `
		ORDER BY (due_date IS NULL), due_date, priority, order_key LIMIT ? OFFSET ?`
	rows, err := s.db.Query(q, append(all, limit, offset)...)
	if err != nil {
		return nil, err
	}
	var out []Task
	var ids []string
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
		ids = append(ids, t.ID)
	}
	err = rows.Err()
	// Close before the next query: the pool holds one connection, so an open
	// result set would block every later read.
	rows.Close()
	if err != nil {
		return nil, err
	}
	byTask, err := s.labelsFor(ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Labels = byTask[out[i].ID]
	}
	return out, nil
}

// Projects returns every live project.
func (s *Store) Projects() ([]Project, error) {
	return s.ProjectsFor("")
}

// ProjectsFor returns the live projects one account may see.
func (s *Store) ProjectsFor(accountID string) ([]Project, error) {
	scope, scopeArgs := scopeFor(accountID, "id")
	rows, err := s.db.Query(`SELECT id, name, color, parent_id, order_key, is_inbox, deleted_at, version
		FROM project WHERE deleted_at IS NULL`+scope+` ORDER BY is_inbox DESC, order_key`, scopeArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		var inbox int
		if err := rows.Scan(&p.ID, &p.Name, &p.Color, &p.ParentID, &p.OrderKey, &inbox, &p.DeletedAt, &p.Version); err != nil {
			return nil, err
		}
		p.IsInbox = inbox == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// Sections returns every live section, ordered inside its project.
func (s *Store) Sections() ([]Section, error) {
	return s.SectionsFor("")
}

// SectionsFor returns the live sections of the projects one account may see.
func (s *Store) SectionsFor(accountID string) ([]Section, error) {
	scope, scopeArgs := scopeFor(accountID, "project_id")
	rows, err := s.db.Query(`SELECT id, project_id, name, order_key, deleted_at, version
		FROM section WHERE deleted_at IS NULL`+scope+` ORDER BY project_id, order_key, name`, scopeArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Section{}
	for rows.Next() {
		var sec Section
		if err := rows.Scan(&sec.ID, &sec.ProjectID, &sec.Name, &sec.OrderKey, &sec.DeletedAt, &sec.Version); err != nil {
			return nil, err
		}
		out = append(out, sec)
	}
	return out, rows.Err()
}

// CommentsFor returns the live comments of one task, oldest first, and only if
// the account may see the task. An empty account reads the whole file.
func (s *Store) CommentsFor(taskID, accountID string) ([]Comment, error) {
	if accountID != "" {
		if _, err := s.TaskFor(taskID, accountID); err != nil {
			return nil, err
		}
	}
	rows, err := s.db.Query(`SELECT `+commentCols+` FROM comment
		WHERE task_id = ? AND deleted_at IS NULL ORDER BY created_at, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Labels returns every live label.
func (s *Store) Labels() ([]Label, error) {
	rows, err := s.db.Query(`SELECT id, name, color, deleted_at, version FROM label WHERE deleted_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Label{}
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Color, &l.DeletedAt, &l.Version); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Task returns one task by id.
// TaskFor returns one task, and only if the account may see it.
func (s *Store) TaskFor(id, accountID string) (Task, error) {
	t, err := s.Task(id)
	if err != nil {
		return t, err
	}
	if accountID == "" {
		return t, nil
	}
	ok, err := s.CanSee(accountID, t.ProjectID)
	if err != nil {
		return Task{}, err
	}
	if !ok {
		return Task{}, ErrDenied
	}
	return t, nil
}

func (s *Store) Task(id string) (Task, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+` FROM task WHERE id = ?`, id)
	if err != nil {
		return Task{}, err
	}
	if !rows.Next() {
		rows.Close()
		return Task{}, ErrNotFound
	}
	t, err := scanTask(rows)
	rows.Close() // free the single connection before the label query
	if err != nil {
		return t, err
	}
	byTask, err := s.labelsFor([]string{id})
	if err != nil {
		return t, err
	}
	t.Labels = byTask[id]
	return t, nil
}

// ProjectByName finds a live project by name, case-insensitive.
func (s *Store) ProjectByName(name string) (Project, error) {
	rows, err := s.db.Query(`SELECT id, name, color, parent_id, order_key, is_inbox, deleted_at, version
		FROM project WHERE deleted_at IS NULL AND lower(name) = lower(?)`, name)
	if err != nil {
		return Project{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Project{}, ErrNotFound
	}
	var p Project
	var inbox int
	err = rows.Scan(&p.ID, &p.Name, &p.Color, &p.ParentID, &p.OrderKey, &inbox, &p.DeletedAt, &p.Version)
	p.IsInbox = inbox == 1
	return p, err
}

// ErrNotFound reports a missing row.
var ErrNotFound = errors.New("not found")

// Search runs the full-text index and returns task ids in rank order.
func (s *Store) Search(text string, limit int) ([]string, error) {
	return s.SearchFor(text, limit, "")
}

// SearchFor is Search for one account. An empty account reads the whole file.
//
// The scope is on the query and not on the caller. A search that read the
// index and then filtered the rows it found would still tell the caller how
// many matches it threw away, through the number it returns.
func (s *Store) SearchFor(text string, limit int, accountID string) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	scope := ""
	args := []any{text}
	if accountID != "" {
		scope = ` AND task_id IN (SELECT id FROM task WHERE project_id IN ` + mineSQL + `)`
		args = append(args, accountID, accountID)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT task_id FROM task_fts WHERE task_fts MATCH ?`+scope+
		` ORDER BY rank LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
