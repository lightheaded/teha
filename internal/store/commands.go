// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	idpkg "github.com/lightheaded/teha/id"
	"github.com/lightheaded/teha/recur"
)

// Command is one client intent. The uuid makes a retry safe.
type Command struct {
	UUID string          `json:"uuid"`
	Type string          `json:"type"`
	Args json.RawMessage `json:"args"`
}

// Result reports what one command did.
type Result struct {
	UUID  string `json:"uuid"`
	OK    bool   `json:"ok"`
	ID    string `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}

// TaskArgs carries the fields of a task. A nil pointer means "leave alone" on
// an update, so a client sends only what changed.
type TaskArgs struct {
	ID           string   `json:"id"`
	ProjectID    *string  `json:"project_id,omitempty"`
	Project      *string  `json:"project,omitempty"` // by name, for MCP and import
	SectionID    *string  `json:"section_id,omitempty"`
	ParentID     *string  `json:"parent_id,omitempty"`
	OrderKey     *string  `json:"order_key,omitempty"`
	Title        *string  `json:"title,omitempty"`
	Description  *string  `json:"description,omitempty"`
	Priority     *int     `json:"priority,omitempty"`
	DueDate      *string  `json:"due_date,omitempty"`
	DueTime      *string  `json:"due_time,omitempty"`
	DueTz        *string  `json:"due_tz,omitempty"` // an IANA name, only meaningful with a time
	RRule        *string  `json:"rrule,omitempty"`
	FromComplete *bool    `json:"rrule_from_completion,omitempty"`
	StartDate    *string  `json:"start_date,omitempty"`
	Deadline     *string  `json:"deadline,omitempty"`
	DurationMin  *int     `json:"duration_min,omitempty"`
	AssigneeID   *string  `json:"assignee_id,omitempty"`
	SourceRef    *string  `json:"source_ref,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Clear        []string `json:"clear,omitempty"` // field names to set back to null
}

// ProjectArgs carries the fields of a project.
type ProjectArgs struct {
	ID       string  `json:"id"`
	Name     *string `json:"name,omitempty"`
	Color    *string `json:"color,omitempty"`
	ParentID *string `json:"parent_id,omitempty"`
	OrderKey *string `json:"order_key,omitempty"`
}

// SectionArgs carries the fields of a section.
type SectionArgs struct {
	ID        string  `json:"id"`
	ProjectID *string `json:"project_id,omitempty"`
	Name      *string `json:"name,omitempty"`
	OrderKey  *string `json:"order_key,omitempty"`
}

// MoveArgs moves one task. The section travels with the project, because a
// section belongs to one project and the pair must agree after the move.
type MoveArgs struct {
	ID        string  `json:"id"`
	ProjectID *string `json:"project_id,omitempty"`
	SectionID *string `json:"section_id,omitempty"` // an empty string means no section
}

// LabelArgs carries the fields of a label.
type LabelArgs struct {
	ID    string  `json:"id"`
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
}

// CommentArgs carries the fields of a comment.
//
// CreatedAt is for the importer only. A Todoist comment has a posting time,
// and an import that stamped every line with the import moment would lose the
// order of a conversation. Nothing else sends it, and the store stamps the
// line itself when it is absent.
type CommentArgs struct {
	ID        string  `json:"id"`
	TaskID    *string `json:"task_id,omitempty"`
	Body      *string `json:"body,omitempty"`
	CreatedAt *string `json:"created_at,omitempty"`
}

// IDArgs addresses one row.
type IDArgs struct {
	ID string `json:"id"`
}

// Event is something that one person has to hear about, and that a write
// produced. The store reports the fact and never the wording: the sender turns
// it into a notification, because the sender owns the transport.
//
// A person is never told about their own action. Assigning a task to yourself
// and answering your own comment are both silent.
type Event struct {
	// Kind is EventAssigned or EventCommented.
	Kind string
	// AccountID is who to tell. ActorID is who did it.
	AccountID string
	ActorID   string
	// Actor is the name of the person who did it, for the wording.
	Actor  string
	TaskID string
	Title  string
	// Body is the text of a comment, and it is empty for an assignment.
	Body string
}

// The kinds of Event.
const (
	EventAssigned  = "assigned"
	EventCommented = "commented"
)

// Apply runs every command in one transaction and returns the new version.
// A command whose uuid was seen before is skipped, so a retry is safe.
//
// Each command runs inside its own savepoint, so one bad command in a batch
// leaves nothing behind. Without the savepoint a command that failed halfway
// kept the rows it had already written: a `task_update` against a missing task
// created the labels it named, then failed, and the account held a label that
// nobody asked for. The version also moved for a command that nobody accepted.
func (s *Store) Apply(cmds []Command) (int64, []Result, error) {
	return s.ApplyAs(cmds, OwnerID)
}

// ApplyAs runs a batch as one account. A command that reaches a project the
// account cannot see fails with one refusal, and the rest of the batch still
// runs: a client sends twenty commands at once, and one bad id must not undo
// the other nineteen.
func (s *Store) ApplyAs(cmds []Command, accountID string) (int64, []Result, error) {
	v, res, _, err := s.ApplyWithEvents(cmds, accountID)
	return v, res, err
}

// ApplyWithEvents is ApplyAs, and it also reports what the other people in the
// household have to hear about. The events are for the notification sender.
//
// They come back from the same call on purpose. A separate scan afterwards
// would have to guess which rows this batch touched, and a batch that was
// rolled back would still produce a notification.
func (s *Store) ApplyWithEvents(cmds []Command, accountID string) (int64, []Result, []Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	act := ownerActor()
	if accountID != "" && accountID != OwnerID {
		a, err := s.AccountByID(accountID)
		if err != nil {
			return 0, nil, nil, err
		}
		act = actor{ID: a.ID, InboxID: a.InboxID}
	}

	results := make([]Result, 0, len(cmds))
	var events []Event
	tx, err := s.db.Begin()
	if err != nil {
		return 0, nil, nil, err
	}
	defer tx.Rollback()

	now := s.stamp()
	today := s.Now().Format("2006-01-02")

	for _, c := range cmds {
		if c.UUID == "" {
			results = append(results, Result{UUID: c.UUID, Error: "missing uuid"})
			continue
		}
		var seen int
		if err := tx.QueryRow(`SELECT count(*) FROM applied_command WHERE uuid = ?`, c.UUID).Scan(&seen); err != nil {
			return 0, nil, nil, err
		}
		if seen > 0 {
			results = append(results, Result{UUID: c.UUID, OK: true})
			continue
		}
		if _, err := tx.Exec(`SAVEPOINT one_command`); err != nil {
			return 0, nil, nil, err
		}
		id, err := applyOne(tx, c, now, today, act, &events)
		if err != nil {
			if _, rerr := tx.Exec(`ROLLBACK TO one_command`); rerr != nil {
				return 0, nil, nil, rerr
			}
			if _, rerr := tx.Exec(`RELEASE one_command`); rerr != nil {
				return 0, nil, nil, rerr
			}
			results = append(results, Result{UUID: c.UUID, Error: err.Error()})
			continue
		}
		if _, err := tx.Exec(`RELEASE one_command`); err != nil {
			return 0, nil, nil, err
		}
		// The log line goes in after the savepoint is released, so a command
		// that failed leaves none. See activity.go.
		recordActivity(tx, c, id, now, act)
		var v int64
		if err := tx.QueryRow(`SELECT max(version) FROM change_log`).Scan(&v); err != nil {
			return 0, nil, nil, err
		}
		if _, err := tx.Exec(`INSERT INTO applied_command (uuid, at, version) VALUES (?,?,?)`, c.UUID, now, v); err != nil {
			return 0, nil, nil, err
		}
		results = append(results, Result{UUID: c.UUID, OK: true, ID: id})
	}

	var v sql.NullInt64
	if err := tx.QueryRow(`SELECT max(version) FROM change_log`).Scan(&v); err != nil {
		return 0, nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, nil, err
	}
	return v.Int64, results, events, nil
}

func applyOne(tx *sql.Tx, c Command, now, today string, act actor, events *[]Event) (string, error) {
	// Every write passes the gate first. See reach.go.
	if err := gate(tx, c, act); err != nil {
		return "", err
	}
	switch c.Type {
	case "task_add":
		var a TaskArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		id, err := taskAdd(tx, a, now, act)
		if err == nil && a.AssigneeID != nil {
			noteAssigned(tx, events, act, a.ID, *a.AssigneeID)
		}
		return id, err
	case "task_update":
		var a TaskArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		// Read who had it BEFORE the write. A command that names the assignee
		// it already has is a repeat, and a repeat must not push again.
		before := ""
		if a.AssigneeID != nil {
			before = assigneeOf(tx, a.ID)
		}
		if err := taskUpdate(tx, a, now); err != nil {
			return a.ID, err
		}
		if a.AssigneeID != nil && *a.AssigneeID != before {
			noteAssigned(tx, events, act, a.ID, *a.AssigneeID)
		}
		return a.ID, nil
	case "task_complete":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, taskComplete(tx, a.ID, now, today, "done")
	case "task_wont_do":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, taskComplete(tx, a.ID, now, today, "wont_do")
	case "task_uncomplete":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, taskSet(tx, a.ID, now, map[string]any{"state": "open", "completed_at": nil})
	case "task_delete":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, taskSet(tx, a.ID, now, map[string]any{"deleted_at": now})
	case "task_restore":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, taskSet(tx, a.ID, now, map[string]any{"deleted_at": nil})
	case "project_add":
		var a ProjectArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return projectAdd(tx, a, now, act)
	case "project_update":
		var a ProjectArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, projectUpdate(tx, a, now)
	case "project_delete":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		if a.ID == InboxID {
			return "", fmt.Errorf("the inbox cannot be deleted")
		}
		if err := rowSet(tx, "project", a.ID, now, map[string]any{"deleted_at": now}); err != nil {
			return "", err
		}
		return a.ID, nil
	case "project_restore":
		// The undo of a project_delete, and the reason the activity log can
		// promise a restore for every row it shows deleted. A task inside the
		// list was never marked deleted, so the list comes back whole.
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, rowSet(tx, "project", a.ID, now, map[string]any{"deleted_at": nil})
	case "reminder_add":
		var a ReminderArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return reminderAdd(tx, a, now, act)
	case "reminder_update":
		var a ReminderArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, reminderUpdate(tx, a, now)
	case "reminder_delete":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		if err := rowSet(tx, "reminder", a.ID, now, map[string]any{"deleted_at": now}); err != nil {
			return "", err
		}
		return a.ID, nil
	case "task_move":
		var a MoveArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, taskMove(tx, a, now)
	case "section_add":
		var a SectionArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return sectionAdd(tx, a, now, act)
	case "section_update":
		var a SectionArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, sectionUpdate(tx, a, now)
	case "section_move":
		var a SectionArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, sectionMove(tx, a, now)
	case "section_reorder":
		var a SectionArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		if a.OrderKey == nil || *a.OrderKey == "" {
			return "", fmt.Errorf("section_reorder needs an order_key")
		}
		return a.ID, rowSet(tx, "section", a.ID, now, map[string]any{"order_key": *a.OrderKey})
	case "section_delete":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, sectionDelete(tx, a.ID, now)
	case "section_restore":
		// The undo of a section_delete. It brings the heading back, and the
		// client then files the tasks with task_move: the server cleared their
		// section and cannot know which tasks were in it.
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return a.ID, rowSet(tx, "section", a.ID, now, map[string]any{"deleted_at": nil})
	case "comment_add":
		var ca CommentArgs
		if err := json.Unmarshal(c.Args, &ca); err != nil {
			return "", err
		}
		id, err := commentAdd(tx, ca, now, act)
		if err == nil {
			noteCommented(tx, events, act, *ca.TaskID, strings.TrimSpace(*ca.Body))
		}
		return id, err
	case "comment_update":
		var ca CommentArgs
		if err := json.Unmarshal(c.Args, &ca); err != nil {
			return "", err
		}
		return ca.ID, commentUpdate(tx, ca, now)
	case "comment_delete":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		if err := rowSet(tx, "comment", a.ID, now, map[string]any{"deleted_at": now}); err != nil {
			return "", err
		}
		return a.ID, nil
	case "label_add":
		var a LabelArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		return labelAdd(tx, a, now)
	case "label_delete":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return "", err
		}
		if err := rowSet(tx, "label", a.ID, now, map[string]any{"deleted_at": now}); err != nil {
			return "", err
		}
		// Every task that carries the label moves to a new version too. Pull
		// hides a deleted label from the label list of a task, so the list of a
		// task changes while the task row does not. A client pulls with
		// `version > since`, so without this it never sees the task again and
		// keeps showing a label that the account deleted.
		if err := touchTasksWithLabel(tx, a.ID, now); err != nil {
			return "", err
		}
		return a.ID, nil
	default:
		return "", fmt.Errorf("unknown command type %q", c.Type)
	}
}

// --- tasks ------------------------------------------------------------------

func taskAdd(tx *sql.Tx, a TaskArgs, now string, act actor) (string, error) {
	if a.ID == "" {
		return "", fmt.Errorf("task_add needs a client id")
	}
	if a.Title == nil || strings.TrimSpace(*a.Title) == "" {
		return "", fmt.Errorf("task_add needs a title")
	}
	// A capture with no project lands in the inbox of the person who wrote it.
	// Each account has one, so the fixed id is only right for the owner.
	projectID := act.InboxID
	if a.ProjectID != nil && *a.ProjectID != "" {
		projectID = *a.ProjectID
	} else if a.Project != nil && *a.Project != "" {
		id, err := projectIDByName(tx, *a.Project)
		if err != nil {
			return "", err
		}
		projectID = id
	}
	order := "m"
	if a.OrderKey != nil {
		order = *a.OrderKey
	}
	prio := 4
	if a.Priority != nil {
		prio = clampPriority(*a.Priority)
	}
	var section any
	if a.SectionID != nil && *a.SectionID != "" {
		if err := checkSection(tx, *a.SectionID, projectID); err != nil {
			return "", err
		}
		section = *a.SectionID
	}
	v, err := bump(tx, "task", a.ID, "insert", now)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(`INSERT INTO task (id, project_id, section_id, parent_id, order_key, title, description,
		priority, due_date, due_time, due_tz, rrule, rrule_from_completion, start_date, deadline,
		duration_min, assignee_id, state, source_ref, created_at, updated_at, version)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'open',?,?,?,?)`,
		a.ID, projectID, section, a.ParentID, order, strings.TrimSpace(*a.Title), str(a.Description),
		prio, a.DueDate, a.DueTime, a.DueTz, a.RRule, boolInt(a.FromComplete), a.StartDate, a.Deadline,
		a.DurationMin, a.AssigneeID, a.SourceRef, now, now, v)
	if err != nil {
		return "", err
	}
	if err := setLabels(tx, a.ID, a.Labels, now); err != nil {
		return "", err
	}
	return a.ID, indexTask(tx, a.ID)
}

func taskUpdate(tx *sql.Tx, a TaskArgs, now string) error {
	set := map[string]any{}
	if a.Title != nil {
		set["title"] = strings.TrimSpace(*a.Title)
	}
	if a.Description != nil {
		set["description"] = *a.Description
	}
	if a.Priority != nil {
		set["priority"] = clampPriority(*a.Priority)
	}
	if a.DueDate != nil {
		set["due_date"] = *a.DueDate
	}
	if a.DueTime != nil {
		set["due_time"] = *a.DueTime
	}
	if a.DueTz != nil {
		set["due_tz"] = *a.DueTz
	}
	if a.RRule != nil {
		set["rrule"] = *a.RRule
	}
	if a.FromComplete != nil {
		set["rrule_from_completion"] = boolInt(a.FromComplete)
	}
	if a.StartDate != nil {
		set["start_date"] = *a.StartDate
	}
	if a.Deadline != nil {
		set["deadline"] = *a.Deadline
	}
	if a.DurationMin != nil {
		set["duration_min"] = *a.DurationMin
	}
	if a.ParentID != nil {
		set["parent_id"] = *a.ParentID
	}
	if a.OrderKey != nil {
		set["order_key"] = *a.OrderKey
	}
	if a.SourceRef != nil {
		set["source_ref"] = *a.SourceRef
	}
	if a.AssigneeID != nil {
		// An empty value means nobody, and nobody is NULL. Two ways to say
		// "unassigned" in one column is a column that every reader has to
		// test twice. The clients send both: the browser clears the field and
		// the phone sends an empty string.
		if *a.AssigneeID == "" {
			set["assignee_id"] = nil
		} else {
			set["assignee_id"] = *a.AssigneeID
		}
	}
	if a.ProjectID != nil {
		set["project_id"] = *a.ProjectID
	} else if a.Project != nil {
		id, err := projectIDByName(tx, *a.Project)
		if err != nil {
			return err
		}
		set["project_id"] = id
	}
	for _, f := range a.Clear {
		switch f {
		case "due_date", "due_time", "due_tz", "rrule", "start_date", "deadline",
			"duration_min", "parent_id", "section_id", "assignee_id":
			set[f] = nil
		default:
			return fmt.Errorf("field %q cannot be cleared", f)
		}
	}
	if a.Labels != nil {
		if err := setLabels(tx, a.ID, a.Labels, now); err != nil {
			return err
		}
	}
	if len(set) == 0 {
		if a.Labels == nil {
			return fmt.Errorf("task_update has nothing to change")
		}
		return touch(tx, "task", a.ID, now)
	}
	if err := rowSet(tx, "task", a.ID, now, set); err != nil {
		return err
	}
	return indexTask(tx, a.ID)
}

// taskComplete closes a task. A recurring task moves to its next date instead
// and stays open, which is what a client expects from a repeating chore.
func taskComplete(tx *sql.Tx, id, now, today, state string) error {
	var rrule sql.NullString
	var due sql.NullString
	var fromCompletion int
	err := tx.QueryRow(`SELECT rrule, due_date, rrule_from_completion FROM task WHERE id = ?`, id).
		Scan(&rrule, &due, &fromCompletion)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if rrule.Valid && rrule.String != "" && state == "done" {
		base := due.String
		if fromCompletion == 1 || base == "" {
			base = today
		}
		next, err := recur.Next(rrule.String, base, today, fromCompletion == 1)
		if err != nil {
			return err
		}
		if next != "" {
			return rowSet(tx, "task", id, now, map[string]any{"due_date": next, "state": "open", "completed_at": nil})
		}
	}
	return rowSet(tx, "task", id, now, map[string]any{"state": state, "completed_at": now})
}

func taskSet(tx *sql.Tx, id, now string, set map[string]any) error {
	return rowSet(tx, "task", id, now, set)
}

// touchTasksWithLabel moves every task that carries one label to a new
// version, so a pull carries the new label list of the task.
func touchTasksWithLabel(tx *sql.Tx, labelID, now string) error {
	rows, err := tx.Query(`SELECT task_id FROM task_label WHERE label_id = ?`, labelID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	// The pool holds one connection, so the result set closes before the write.
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := touch(tx, "task", id, now); err != nil {
			return err
		}
	}
	return nil
}

// --- projects and labels ----------------------------------------------------

func projectAdd(tx *sql.Tx, a ProjectArgs, now string, act actor) (string, error) {
	if a.ID == "" || a.Name == nil || strings.TrimSpace(*a.Name) == "" {
		return "", fmt.Errorf("project_add needs an id and a name")
	}
	order := "m"
	if a.OrderKey != nil {
		order = *a.OrderKey
	}
	color := "grey"
	if a.Color != nil {
		color = *a.Color
	}
	v, err := bump(tx, "project", a.ID, "insert", now)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(`INSERT INTO project (id, name, color, parent_id, order_key, is_inbox,
		owner_id, created_at, updated_at, version) VALUES (?,?,?,?,?,0,?,?,?,?)`,
		a.ID, strings.TrimSpace(*a.Name), color, a.ParentID, order, act.ID, now, now, v)
	return a.ID, err
}

func projectUpdate(tx *sql.Tx, a ProjectArgs, now string) error {
	set := map[string]any{}
	if a.Name != nil {
		set["name"] = strings.TrimSpace(*a.Name)
	}
	if a.Color != nil {
		set["color"] = *a.Color
	}
	if a.ParentID != nil {
		set["parent_id"] = *a.ParentID
	}
	if a.OrderKey != nil {
		set["order_key"] = *a.OrderKey
	}
	if len(set) == 0 {
		return fmt.Errorf("project_update has nothing to change")
	}
	return rowSet(tx, "project", a.ID, now, set)
}

func labelAdd(tx *sql.Tx, a LabelArgs, now string) (string, error) {
	if a.ID == "" || a.Name == nil {
		return "", fmt.Errorf("label_add needs an id and a name")
	}
	color := "grey"
	if a.Color != nil {
		color = *a.Color
	}
	v, err := bump(tx, "label", a.ID, "insert", now)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(`INSERT INTO label (id, name, color, created_at, updated_at, version)
		VALUES (?,?,?,?,?,?)`, a.ID, strings.TrimSpace(*a.Name), color, now, now, v)
	return a.ID, err
}

// setLabels replaces the label set of a task. A name with no label row creates
// one, so a quick add never fails on an unknown label.
func setLabels(tx *sql.Tx, taskID string, names []string, now string) error {
	if _, err := tx.Exec(`DELETE FROM task_label WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for _, raw := range names {
		name := strings.TrimSpace(strings.TrimPrefix(raw, "@"))
		if name == "" {
			continue
		}
		var id string
		err := tx.QueryRow(`SELECT id FROM label WHERE lower(name) = lower(?) AND deleted_at IS NULL`, name).Scan(&id)
		if err == sql.ErrNoRows {
			id = idpkg.New("l")
			v, err := bump(tx, "label", id, "insert", now)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO label (id, name, color, created_at, updated_at, version)
				VALUES (?,?,?,?,?,?)`, id, name, "grey", now, now, v); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO task_label (task_id, label_id) VALUES (?,?)`, taskID, id); err != nil {
			return err
		}
	}
	return nil
}

// projectIDByName resolves a project by name. An exact match wins. Otherwise a
// unique prefix match wins, so "#Trip" finds "Trip to Setomaa" the way a person
// expects while typing. An ambiguous prefix is an error, never a guess.
func projectIDByName(tx *sql.Tx, name string) (string, error) {
	name = strings.TrimSpace(strings.TrimPrefix(name, "#"))
	if name == "" {
		return InboxID, nil
	}
	if strings.EqualFold(name, "inbox") {
		return InboxID, nil
	}
	var id string
	err := tx.QueryRow(`SELECT id FROM project WHERE lower(name) = lower(?) AND deleted_at IS NULL`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	rows, err := tx.Query(`SELECT id, name FROM project
		WHERE lower(name) LIKE lower(?) || '%' AND deleted_at IS NULL ORDER BY name`, name)
	if err != nil {
		return "", err
	}
	var ids, names []string
	for rows.Next() {
		var i, n string
		if err := rows.Scan(&i, &n); err != nil {
			rows.Close()
			return "", err
		}
		ids = append(ids, i)
		names = append(names, n)
	}
	rows.Close()
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no project named %q", name)
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("%q matches %s: use the full name", name, strings.Join(names, ", "))
	}
}

// --- sections ---------------------------------------------------------------
// A section is a heading inside one project, and a column on the board layout.
// Every command here joins the change log through bump or rowSet, exactly as a
// project command does, so a client pulls a section the same way it pulls a
// project.

func sectionAdd(tx *sql.Tx, a SectionArgs, now string, act actor) (string, error) {
	if a.ID == "" || a.Name == nil || strings.TrimSpace(*a.Name) == "" {
		return "", fmt.Errorf("section_add needs an id and a name")
	}
	project := act.InboxID
	if a.ProjectID != nil && *a.ProjectID != "" {
		project = *a.ProjectID
	}
	order := "m"
	if a.OrderKey != nil {
		order = *a.OrderKey
	}
	v, err := bump(tx, "section", a.ID, "insert", now)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(`INSERT INTO section (id, project_id, name, order_key,
		created_at, updated_at, version) VALUES (?,?,?,?,?,?,?)`,
		a.ID, project, strings.TrimSpace(*a.Name), order, now, now, v)
	return a.ID, err
}

func sectionUpdate(tx *sql.Tx, a SectionArgs, now string) error {
	set := map[string]any{}
	if a.Name != nil {
		if strings.TrimSpace(*a.Name) == "" {
			return fmt.Errorf("a section needs a name")
		}
		set["name"] = strings.TrimSpace(*a.Name)
	}
	if a.OrderKey != nil {
		set["order_key"] = *a.OrderKey
	}
	if len(set) == 0 {
		return fmt.Errorf("section_update has nothing to change")
	}
	return rowSet(tx, "section", a.ID, now, set)
}

// sectionMove carries a section to another project, and takes its tasks along.
//
// The tasks have to travel. A section belongs to one project, so a task left
// behind would name a section in a project it is not in, and no board could
// draw it. Each task gets its own change_log row, so every client learns the
// new project the same way it learns any other field.
func sectionMove(tx *sql.Tx, a SectionArgs, now string) error {
	if a.ProjectID == nil || *a.ProjectID == "" {
		return fmt.Errorf("section_move needs a project_id")
	}
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM project WHERE id = ? AND deleted_at IS NULL`,
		*a.ProjectID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no project with id %q", *a.ProjectID)
	}
	set := map[string]any{"project_id": *a.ProjectID}
	if a.OrderKey != nil {
		set["order_key"] = *a.OrderKey
	}
	if err := rowSet(tx, "section", a.ID, now, set); err != nil {
		return err
	}
	ids, err := tasksInSection(tx, a.ID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := rowSet(tx, "task", id, now, map[string]any{"project_id": *a.ProjectID}); err != nil {
			return err
		}
	}
	return nil
}

// sectionDelete removes a heading and keeps the work.
//
// Every task of the section stays in the project and loses its section, so it
// appears in the column for tasks with no section. A section is a heading, and
// the removal of a heading must never hide or delete work. The alternative, a
// task that still points at a deleted section, is a row that the board cannot
// draw and that no column lists.
//
// The delete is soft, like every other delete here, and each task carries its
// own change_log row, so a client that pulls learns both changes.
func sectionDelete(tx *sql.Tx, id, now string) error {
	ids, err := tasksInSection(tx, id)
	if err != nil {
		return err
	}
	if err := rowSet(tx, "section", id, now, map[string]any{"deleted_at": now}); err != nil {
		return err
	}
	for _, task := range ids {
		if err := rowSet(tx, "task", task, now, map[string]any{"section_id": nil}); err != nil {
			return err
		}
	}
	return nil
}

func tasksInSection(tx *sql.Tx, id string) ([]string, error) {
	rows, err := tx.Query(`SELECT id FROM task WHERE section_id = ?`, id)
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		var task string
		if err := rows.Scan(&task); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, task)
	}
	err = rows.Err()
	rows.Close() // the pool holds one connection, so free it before the writes
	return out, err
}

// taskMove writes the project and the section of one task in one command.
//
// The two fields travel together on purpose. A board drag says "this task now
// belongs here", and a client that sent two commands could land the section
// before the project and leave the pair in disagreement for one round trip.
func taskMove(tx *sql.Tx, a MoveArgs, now string) error {
	if a.ProjectID == nil && a.SectionID == nil {
		return fmt.Errorf("task_move needs a project_id or a section_id")
	}
	project := ""
	if a.ProjectID != nil && *a.ProjectID != "" {
		project = *a.ProjectID
	} else {
		err := tx.QueryRow(`SELECT project_id FROM task WHERE id = ?`, a.ID).Scan(&project)
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	}
	set := map[string]any{"project_id": project}
	if a.SectionID != nil && *a.SectionID != "" {
		if err := checkSection(tx, *a.SectionID, project); err != nil {
			return err
		}
		set["section_id"] = *a.SectionID
	} else {
		set["section_id"] = nil
	}
	return rowSet(tx, "task", a.ID, now, set)
}

// checkSection refuses a section that is gone, and one that belongs to another
// project. Without it a task could sit in a column of a project it is not in.
func checkSection(tx *sql.Tx, sectionID, projectID string) error {
	var owner string
	err := tx.QueryRow(`SELECT project_id FROM section WHERE id = ? AND deleted_at IS NULL`, sectionID).Scan(&owner)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no section with id %q", sectionID)
	}
	if err != nil {
		return err
	}
	if owner != projectID {
		return fmt.Errorf("section %q is in another project", sectionID)
	}
	return nil
}

// --- who hears about it -----------------------------------------------------
// A write tells the other people in the household. The store reports the fact
// and the sender writes the words: see Event.

// noteAssigned records that somebody now has a task. Giving yourself a task is
// silent, and so is taking an assignee away.
func noteAssigned(tx *sql.Tx, events *[]Event, act actor, taskID, assignee string) {
	if events == nil || assignee == "" || assignee == act.ID {
		return
	}
	*events = append(*events, Event{
		Kind: EventAssigned, AccountID: assignee, ActorID: act.ID,
		Actor: nameOf(tx, act.ID), TaskID: taskID, Title: titleOf(tx, taskID),
	})
}

// noteCommented records that somebody said something. Everybody who can see
// the task hears it, and the author does not hear their own line.
func noteCommented(tx *sql.Tx, events *[]Event, act actor, taskID, body string) {
	if events == nil || body == "" {
		return
	}
	project := projectOfRow(tx, "task", taskID)
	if project == "" {
		return
	}
	title := titleOf(tx, taskID)
	actorName := nameOf(tx, act.ID)
	for _, who := range accountsWhoSee(tx, project) {
		if who == act.ID {
			continue
		}
		*events = append(*events, Event{
			Kind: EventCommented, AccountID: who, ActorID: act.ID,
			Actor: actorName, TaskID: taskID, Title: title, Body: body,
		})
	}
}

// accountsWhoSee lists the people a project reaches: the owner, and everybody
// it is shared with.
func accountsWhoSee(tx *sql.Tx, projectID string) []string {
	rows, err := tx.Query(`SELECT owner_id FROM project WHERE id = ? AND owner_id IS NOT NULL
		UNION SELECT account_id FROM project_member WHERE project_id = ?`, projectID, projectID)
	if err != nil {
		return nil
	}
	var out []string
	for rows.Next() {
		var who string
		if err := rows.Scan(&who); err != nil {
			rows.Close()
			return out
		}
		out = append(out, who)
	}
	rows.Close() // the pool holds one connection, so free it before any write
	return out
}

func titleOf(tx *sql.Tx, taskID string) string {
	var title string
	if err := tx.QueryRow(`SELECT title FROM task WHERE id = ?`, taskID).Scan(&title); err != nil {
		return ""
	}
	return title
}

func assigneeOf(tx *sql.Tx, taskID string) string {
	var who sql.NullString
	if err := tx.QueryRow(`SELECT assignee_id FROM task WHERE id = ?`, taskID).Scan(&who); err != nil {
		return ""
	}
	return who.String
}

// nameOf is the name a person reads, and never an account id.
func nameOf(tx *sql.Tx, accountID string) string {
	var name, display string
	err := tx.QueryRow(`SELECT name, display_name FROM account WHERE id = ?`, accountID).
		Scan(&name, &display)
	if err != nil {
		return ""
	}
	if display != "" {
		return display
	}
	return name
}

// --- comments ---------------------------------------------------------------
// A comment is a line of talk on one task. It joins the change log through
// bump and rowSet, so a client pulls a comment exactly as it pulls a task.

func commentAdd(tx *sql.Tx, a CommentArgs, now string, act actor) (string, error) {
	if a.ID == "" {
		return "", fmt.Errorf("comment_add needs a client id")
	}
	if a.TaskID == nil || *a.TaskID == "" {
		return "", fmt.Errorf("comment_add needs a task_id")
	}
	if a.Body == nil || strings.TrimSpace(*a.Body) == "" {
		return "", fmt.Errorf("comment_add needs a body")
	}
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM task WHERE id = ?`, *a.TaskID).Scan(&n); err != nil {
		return "", err
	}
	if n == 0 {
		return "", fmt.Errorf("no task with id %q", *a.TaskID)
	}
	created := now
	if a.CreatedAt != nil && *a.CreatedAt != "" {
		created = *a.CreatedAt
	}
	v, err := bump(tx, "comment", a.ID, "insert", now)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(`INSERT INTO comment (id, task_id, account_id, body,
		created_at, updated_at, version) VALUES (?,?,?,?,?,?,?)`,
		a.ID, *a.TaskID, act.ID, strings.TrimSpace(*a.Body), created, now, v)
	return a.ID, err
}

func commentUpdate(tx *sql.Tx, a CommentArgs, now string) error {
	if a.Body == nil || strings.TrimSpace(*a.Body) == "" {
		return fmt.Errorf("comment_update needs a body")
	}
	return rowSet(tx, "comment", a.ID, now, map[string]any{"body": strings.TrimSpace(*a.Body)})
}

// --- generic row helpers ----------------------------------------------------

// rowSet writes named columns, bumps the version and stamps updated_at.
func rowSet(tx *sql.Tx, table, id, now string, set map[string]any) error {
	if len(set) == 0 {
		return nil
	}
	v, err := bump(tx, table, id, "update", now)
	if err != nil {
		return err
	}
	cols := make([]string, 0, len(set)+2)
	args := make([]any, 0, len(set)+3)
	for _, k := range sortedKeys(set) {
		cols = append(cols, k+" = ?")
		args = append(args, set[k])
	}
	cols = append(cols, "updated_at = ?", "version = ?")
	args = append(args, now, v, id)
	res, err := tx.Exec(`UPDATE `+table+` SET `+strings.Join(cols, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func touch(tx *sql.Tx, table, id, now string) error {
	return rowSet(tx, table, id, now, map[string]any{"updated_at": now})
}

// indexTask refreshes the full-text row for a task.
func indexTask(tx *sql.Tx, id string) error {
	var title, desc string
	err := tx.QueryRow(`SELECT title, description FROM task WHERE id = ?`, id).Scan(&title, &desc)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM task_fts WHERE task_id = ?`, id); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO task_fts (task_id, title, description) VALUES (?,?,?)`, id, title, desc)
	return err
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ { // small maps, an insertion sort is enough
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func clampPriority(p int) int {
	if p < 1 {
		return 1
	}
	if p > 4 {
		return 4
	}
	return p
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func boolInt(p *bool) int {
	if p != nil && *p {
		return 1
	}
	return 0
}
