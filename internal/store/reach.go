// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// The gate that every write passes through in a household.
//
// One question decides everything: which projects does this command touch? A
// task, a section and a reminder all hang off a project, so an account that
// may see the project may write the row, and an account that may not gets one
// refusal with no detail in it.
//
// The gate is here, and not spread through the fifty cases of applyOne. A
// check that lives beside each case is a check that the next case forgets.

// actor is the account that is applying a batch of commands.
type actor struct {
	ID      string
	InboxID string
}

// ownerActor is the actor of a file that has never been shared, and of every
// caller that does not name one.
func ownerActor() actor { return actor{ID: OwnerID, InboxID: InboxID} }

// reach lists the projects one command touches, and says whether the account
// must own them rather than only see them.
type reach struct {
	projects  []string
	ownerOnly bool
	// assignee is a person the command wants to give a task to. They must be
	// able to see the same projects: a task given to somebody who cannot see
	// the list is a task they can never open, and the notification about it
	// carries a title they may not read.
	assignee string
}

// commandReach reads the arguments of a command and answers what it touches.
// An id that names no row gives no project, and the command itself then fails
// with its own message: a refusal must never be the way a caller learns that a
// row does not exist.
func commandReach(tx *sql.Tx, c Command, act actor) (reach, error) {
	var r reach
	add := func(id string) {
		if id != "" {
			r.projects = append(r.projects, id)
		}
	}

	switch c.Type {
	case "task_add":
		var a TaskArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return r, err
		}
		add(targetProject(tx, a, act))
		if a.AssigneeID != nil {
			r.assignee = *a.AssigneeID
		}

	case "task_update", "task_move":
		// Two projects can be in play: where the task is now, and where it is
		// going. Both must be reachable, or a task could be pushed into a list
		// the account cannot see, or pulled out of one.
		var a TaskArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return r, err
		}
		add(projectOfRow(tx, "task", a.ID))
		if a.ProjectID != nil && *a.ProjectID != "" {
			add(*a.ProjectID)
		}
		if a.SectionID != nil && *a.SectionID != "" {
			add(projectOfRow(tx, "section", *a.SectionID))
		}
		if a.AssigneeID != nil {
			r.assignee = *a.AssigneeID
		}

	case "task_complete", "task_wont_do", "task_uncomplete", "task_delete", "task_restore":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return r, err
		}
		add(projectOfRow(tx, "task", a.ID))

	case "project_add":
		// A person may always make a list of their own.

	case "project_update", "project_delete":
		var a struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return r, err
		}
		add(a.ID)
		// Renaming or deleting a shared list is the owner's to do. A member
		// works inside it and does not take it away from everybody.
		r.ownerOnly = true

	case "section_add":
		var a SectionArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return r, err
		}
		if a.ProjectID != nil && *a.ProjectID != "" {
			add(*a.ProjectID)
		} else {
			add(act.InboxID)
		}

	case "section_update", "section_delete":
		var a struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return r, err
		}
		add(projectOfRow(tx, "section", a.ID))

	case "comment_add":
		var a CommentArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return r, err
		}
		if a.TaskID != nil && *a.TaskID != "" {
			add(projectOfRow(tx, "task", *a.TaskID))
		}

	case "comment_update", "comment_delete":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return r, err
		}
		// Two tests, and both must pass. The project has to be reachable, and
		// the line has to be this person's own: a shared list lets two people
		// talk, and neither one edits what the other said.
		var author string
		var task string
		err := tx.QueryRow(`SELECT account_id, task_id FROM comment WHERE id = ?`, a.ID).
			Scan(&author, &task)
		if err == nil {
			if author != act.ID {
				return r, fmt.Errorf("%w: that comment is somebody else's", ErrDenied)
			}
			add(projectOfRow(tx, "task", task))
		}

	case "reminder_add":
		var a ReminderArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return r, err
		}
		if a.TaskID != nil && *a.TaskID != "" {
			add(projectOfRow(tx, "task", *a.TaskID))
		}

	case "reminder_update", "reminder_delete":
		var a IDArgs
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return r, err
		}
		// A reminder belongs to one person, not to a project. The owner of the
		// row is the test, and a reminder of somebody else is out of reach
		// whatever the task says.
		var owner sql.NullString
		err := tx.QueryRow(`SELECT account_id FROM reminder WHERE id = ?`, a.ID).Scan(&owner)
		if err == nil && owner.Valid && owner.String != "" && owner.String != act.ID {
			return r, ErrDenied
		}

	default:
		// A label is household vocabulary, and it belongs to no project.
	}
	return r, nil
}

// targetProject says where a new task lands: the project it names, the project
// whose name it gives, or the inbox of the account that is writing it.
func targetProject(tx *sql.Tx, a TaskArgs, act actor) string {
	if a.ProjectID != nil && *a.ProjectID != "" {
		return *a.ProjectID
	}
	if a.Project != nil && *a.Project != "" {
		if id, err := projectIDByName(tx, *a.Project); err == nil {
			return id
		}
		// An unknown name is the command's own error to report, not a refusal.
		return ""
	}
	if a.ParentID != nil && *a.ParentID != "" {
		return projectOfRow(tx, "task", *a.ParentID)
	}
	return act.InboxID
}

// projectOfRow finds the project a task or a section belongs to. An unknown id
// gives an empty string, so the command reports the missing row itself.
func projectOfRow(tx *sql.Tx, table, id string) string {
	if id == "" {
		return ""
	}
	var project string
	// The table name is a literal from the switch above and never from input.
	if err := tx.QueryRow(`SELECT project_id FROM `+table+` WHERE id = ?`, id).Scan(&project); err != nil {
		return ""
	}
	return project
}

// gate refuses a command that reaches beyond what the account may touch.
func gate(tx *sql.Tx, c Command, act actor) error {
	r, err := commandReach(tx, c, act)
	if err != nil {
		return err
	}
	for _, projectID := range r.projects {
		ok, err := canSee(tx, act.ID, projectID)
		if err != nil {
			return err
		}
		if !ok {
			// The same answer whether the project is somebody else's or does
			// not exist. A refusal that tells the two apart is a way to ask
			// which lists the household holds.
			return ErrDenied
		}
		if !r.ownerOnly {
			continue
		}
		var owner sql.NullString
		if err := tx.QueryRow(`SELECT owner_id FROM project WHERE id = ?`, projectID).Scan(&owner); err != nil {
			return err
		}
		if owner.Valid && owner.String != "" && owner.String != act.ID {
			return fmt.Errorf("%w: that list belongs to somebody else", ErrDenied)
		}
	}
	if r.assignee == "" || r.assignee == act.ID {
		return nil
	}
	for _, projectID := range r.projects {
		ok, err := canSee(tx, r.assignee, projectID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: that person cannot see that list", ErrDenied)
		}
	}
	return nil
}
