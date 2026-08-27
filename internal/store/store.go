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
	if err := s.seedInbox(); err != nil {
		return nil, err
	}
	if err := s.seedOwner(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

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
		created_at, updated_at, version) VALUES (?,?,?,NULL,?,1,?,?,?)`,
		InboxID, "Inbox", "grey", "m", now, now, v)
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

// Label is a tag on a task.
type Label struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Color     string  `json:"color"`
	DeletedAt *string `json:"deleted_at,omitempty"`
	Version   int64   `json:"v"`
}

// Task is one item of work.
type Task struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"project_id"`
	ParentID     *string  `json:"parent_id,omitempty"`
	OrderKey     string   `json:"order_key"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Priority     int      `json:"priority"`
	DueDate      *string  `json:"due_date,omitempty"`
	DueTime      *string  `json:"due_time,omitempty"`
	DueTz        *string  `json:"due_tz,omitempty"`
	RRule        *string  `json:"rrule,omitempty"`
	FromComplete bool     `json:"rrule_from_completion,omitempty"`
	StartDate    *string  `json:"start_date,omitempty"`
	Deadline     *string  `json:"deadline,omitempty"`
	DurationMin  *int     `json:"duration_min,omitempty"`
	State        string   `json:"state"`
	CompletedAt  *string  `json:"completed_at,omitempty"`
	DeletedAt    *string  `json:"deleted_at,omitempty"`
	SourceRef    *string  `json:"source_ref,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Version      int64    `json:"v"`
}

const taskCols = `id, project_id, parent_id, order_key, title, description, priority,
	due_date, due_time, due_tz, rrule, rrule_from_completion, start_date, deadline, duration_min,
	state, completed_at, deleted_at, source_ref, version`

func scanTask(rows *sql.Rows) (Task, error) {
	var t Task
	var fromComplete int
	err := rows.Scan(&t.ID, &t.ProjectID, &t.ParentID, &t.OrderKey, &t.Title, &t.Description,
		&t.Priority, &t.DueDate, &t.DueTime, &t.DueTz, &t.RRule, &fromComplete, &t.StartDate, &t.Deadline,
		&t.DurationMin, &t.State, &t.CompletedAt, &t.DeletedAt, &t.SourceRef, &t.Version)
	t.FromComplete = fromComplete == 1
	return t, err
}

// --- reads ------------------------------------------------------------------

// Delta is everything that changed after a version.
type Delta struct {
	Version   int64      `json:"version"`
	Projects  []Project  `json:"projects"`
	Labels    []Label    `json:"labels"`
	Tasks     []Task     `json:"tasks"`
	Reminders []Reminder `json:"reminders"`
}

// Pull returns every row with a version above since.
func (s *Store) Pull(since int64) (Delta, error) {
	d := Delta{Projects: []Project{}, Labels: []Label{}, Tasks: []Task{}, Reminders: []Reminder{}}
	v, err := s.Version()
	if err != nil {
		return d, err
	}
	d.Version = v

	rows, err := s.db.Query(`SELECT id, name, color, parent_id, order_key, is_inbox, deleted_at, version
		FROM project WHERE version > ? ORDER BY version`, since)
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

	rows, err = s.db.Query(`SELECT `+taskCols+` FROM task WHERE version > ? ORDER BY version`, since)
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

	rows, err = s.db.Query(`SELECT `+reminderCols+` FROM reminder WHERE version > ? ORDER BY version`, since)
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
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT ` + taskCols + ` FROM task WHERE deleted_at IS NULL AND (` + where + `)
		ORDER BY (due_date IS NULL), due_date, priority, order_key LIMIT ? OFFSET ?`
	rows, err := s.db.Query(q, append(append([]any{}, args...), limit, offset)...)
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
	rows, err := s.db.Query(`SELECT id, name, color, parent_id, order_key, is_inbox, deleted_at, version
		FROM project WHERE deleted_at IS NULL ORDER BY is_inbox DESC, order_key`)
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
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT task_id FROM task_fts WHERE task_fts MATCH ? ORDER BY rank LIMIT ?`, text, limit)
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
