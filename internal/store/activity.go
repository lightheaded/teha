// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
)

// The activity log: who did what, and when.
//
// One table serves two readers. A person opens a project and reads its
// history, and the same rows are the audit trail that §6.6 of docs/PLAN.md
// asks for: every login, every token and every failed attempt.
//
// The store writes the fact and never the sentence, exactly as it does for a
// notification: a row holds an action name, an id and the title the row had at
// that moment, and the client writes the words. See docs/DECISIONS.md D-020.
//
// Recording happens in ONE place, right after a command succeeds, and not
// inside the fifty cases of applyOne. A log line that lives beside each case is
// a log line that the next case forgets.

// Activity is one line of the log.
type Activity struct {
	Seq       int64  `json:"seq"`
	At        string `json:"at"`
	AccountID string `json:"account_id"`
	// Actor is the name of the person, so a client never has to resolve an id
	// it may not be allowed to look up.
	Actor     string `json:"actor,omitempty"`
	Action    string `json:"action"`
	ProjectID string `json:"project_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Title     string `json:"title,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Addr      string `json:"addr,omitempty"`
}

// The actions that are not commands. A command writes its own type as the
// action, so only the security actions need names here.
const (
	ActionLogin         = "login"
	ActionLoginFailed   = "login_failed"
	ActionLogout        = "logout"
	ActionPasskeyAdd    = "passkey_add"
	ActionPasskeyDelete = "passkey_delete"
	ActionInviteCreate  = "invite_create"
	ActionInviteRevoke  = "invite_revoke"
	ActionJoined        = "joined"
	ActionShare         = "share"
	ActionUnshare       = "unshare"
)

// ActivityQuery asks for one page of the log.
//
// Before is the paging cursor: the answer holds rows below it, newest first,
// so the next page asks with the smallest seq it received. Zero means the
// newest page.
type ActivityQuery struct {
	ProjectID string
	TaskID    string
	Before    int64
	Limit     int
}

// maxActivityPage caps a page, so a client cannot ask for the whole log in one
// request.
const maxActivityPage = 200

// ActivityFor reads one page of the log that an account may see.
//
// A row is visible through its project, exactly as a task is. A row with no
// project is personal: a login, a token and a reminder belong to one person
// and nobody else reads them, not even the owner of the household.
func (s *Store) ActivityFor(q ActivityQuery, accountID string) ([]Activity, error) {
	limit := q.Limit
	if limit <= 0 || limit > maxActivityPage {
		limit = maxActivityPage
	}

	const mine = `(SELECT id FROM project WHERE owner_id = ?
		UNION SELECT project_id FROM project_member WHERE account_id = ?)`

	where := `(a.project_id IN ` + mine + ` OR (a.project_id IS NULL AND a.account_id = ?))`
	args := []any{accountID, accountID, accountID}

	if q.ProjectID != "" {
		where += ` AND a.project_id = ?`
		args = append(args, q.ProjectID)
	}
	if q.TaskID != "" {
		where += ` AND a.task_id = ?`
		args = append(args, q.TaskID)
	}
	if q.Before > 0 {
		where += ` AND a.seq < ?`
		args = append(args, q.Before)
	}
	args = append(args, limit)

	rows, err := s.db.Query(`SELECT a.seq, a.at, a.account_id,
		coalesce(nullif(c.display_name, ''), c.name, ''),
		a.action, coalesce(a.project_id, ''), coalesce(a.task_id, ''), a.title, a.detail, a.addr
		FROM activity a LEFT JOIN account c ON c.id = a.account_id
		WHERE `+where+` ORDER BY a.seq DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Activity{}
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.Seq, &a.At, &a.AccountID, &a.Actor, &a.Action,
			&a.ProjectID, &a.TaskID, &a.Title, &a.Detail, &a.Addr); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Note records one action that is not a command: a login, a passkey, an
// invitation. It is the audit half of §6.6.
//
// It fails quietly on purpose. A login that works must not fail because the
// log could not be written, and a login that is refused must still be refused.
// The caller has nothing useful to do with the error either way.
func (s *Store) Note(accountID, action, title, detail, addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(`INSERT INTO activity (at, account_id, action, title, detail, addr)
		VALUES (?,?,?,?,?,?)`, s.stamp(), accountID, action, title, detail, addr)
}

// NoteIn records an action against one project, so the other people who share
// the list read it too. Sharing a list is the example: it is not a command,
// and it is not private either.
func (s *Store) NoteIn(accountID, action, projectID, title, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(`INSERT INTO activity (at, account_id, action, project_id, title, detail)
		VALUES (?,?,?,?,?,?)`, s.stamp(), accountID, action, projectID, title, detail)
}

// recordActivity writes the log line for a command that has just succeeded.
//
// It runs inside the command's transaction, after the savepoint was released,
// so a command that failed leaves no line. It reads the row back rather than
// trusting the arguments: a task_add that took its project from a name, and a
// task_update that changed nothing, both have to log where the row actually
// is.
//
// It never fails the command. A log that cannot be written is a lost line, and
// losing the write itself is worse.
func recordActivity(tx *sql.Tx, c Command, id, now string, act actor) {
	if id == "" {
		return
	}
	var projectID, taskID, title sql.NullString
	detail := ""

	switch {
	case strings.HasPrefix(c.Type, "task_"):
		taskID = sql.NullString{String: id, Valid: true}
		var p, t string
		if err := tx.QueryRow(`SELECT project_id, title FROM task WHERE id = ?`, id).Scan(&p, &t); err != nil {
			return
		}
		projectID = sql.NullString{String: p, Valid: true}
		title = sql.NullString{String: t, Valid: true}
		if c.Type == "task_update" {
			detail = changedFields(c.Args)
		}

	case strings.HasPrefix(c.Type, "project_"):
		var name string
		if err := tx.QueryRow(`SELECT name FROM project WHERE id = ?`, id).Scan(&name); err != nil {
			return
		}
		projectID = sql.NullString{String: id, Valid: true}
		title = sql.NullString{String: name, Valid: true}
		if c.Type == "project_update" {
			detail = changedFields(c.Args)
		}

	case strings.HasPrefix(c.Type, "section_"):
		var p, name string
		if err := tx.QueryRow(`SELECT project_id, name FROM section WHERE id = ?`, id).Scan(&p, &name); err != nil {
			return
		}
		projectID = sql.NullString{String: p, Valid: true}
		title = sql.NullString{String: name, Valid: true}

	case strings.HasPrefix(c.Type, "comment_"):
		// The line is logged, and its text is not. A comment is a row that a
		// client can read, and a deleted comment whose words survive in a log
		// nobody can delete is not what a person means by deleting it.
		var t string
		if err := tx.QueryRow(`SELECT task_id FROM comment WHERE id = ?`, id).Scan(&t); err != nil {
			return
		}
		var p, name string
		if err := tx.QueryRow(`SELECT project_id, title FROM task WHERE id = ?`, t).Scan(&p, &name); err != nil {
			return
		}
		taskID = sql.NullString{String: t, Valid: true}
		projectID = sql.NullString{String: p, Valid: true}
		title = sql.NullString{String: name, Valid: true}

	case strings.HasPrefix(c.Type, "reminder_"):
		// A reminder belongs to one person even on a shared task, so its line
		// carries no project and only its owner reads it.
		var t sql.NullString
		if err := tx.QueryRow(`SELECT task_id FROM reminder WHERE id = ?`, id).Scan(&t); err != nil {
			return
		}
		taskID = t
		if t.Valid && t.String != "" {
			var name string
			if err := tx.QueryRow(`SELECT title FROM task WHERE id = ?`, t.String).Scan(&name); err == nil {
				title = sql.NullString{String: name, Valid: true}
			}
		}

	case strings.HasPrefix(c.Type, "label_"):
		// A label is household vocabulary and belongs to no project, so its
		// line is personal to whoever wrote it.
		var name string
		if err := tx.QueryRow(`SELECT name FROM label WHERE id = ?`, id).Scan(&name); err != nil {
			return
		}
		title = sql.NullString{String: name, Valid: true}

	default:
		return
	}

	_, _ = tx.Exec(`INSERT INTO activity (at, account_id, action, project_id, task_id, title, detail)
		VALUES (?,?,?,?,?,?,?)`, now, act.ID, c.Type, projectID, taskID, title.String, detail)
}

// changedFields names the fields an update command asked to change, in one
// sorted comma-separated string.
//
// It reads the argument object rather than a typed struct, so a field added to
// a command appears in the log with no change here. `clear` is expanded,
// because a person reads "the deadline went away" and not "a clear happened".
func changedFields(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	seen := map[string]bool{}
	if c, ok := m["clear"]; ok {
		var names []string
		if err := json.Unmarshal(c, &names); err == nil {
			for _, n := range names {
				seen[n] = true
			}
		}
	}
	for k := range m {
		if k == "id" || k == "clear" {
			continue
		}
		seen[k] = true
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
