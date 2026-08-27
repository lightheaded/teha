// SPDX-License-Identifier: AGPL-3.0-or-later

// Package todoist reads a Todoist account through the API v1 sync endpoint and
// writes it into a teha store.
//
// The package holds three parts: a paced HTTP client, a natural language to
// RRULE converter, and the mapper that turns a sync payload into store
// commands. The mapper writes only through store.Apply, so the import obeys
// the same rules as every other write path.
package todoist

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ID is a Todoist object id. The API v1 sends string ids, but older payloads
// and some fixtures send numbers, so both forms decode here.
type ID string

func (i *ID) UnmarshalJSON(b []byte) error {
	text := strings.TrimSpace(string(b))
	if text == "null" {
		*i = ""
		return nil
	}
	if strings.HasPrefix(text, `"`) {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*i = ID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*i = ID(n.String())
	return nil
}

func (i ID) String() string { return string(i) }

// Bool decodes a flag that the API sends as true, false, 1 or 0.
type Bool bool

func (v *Bool) UnmarshalJSON(b []byte) error {
	text := strings.TrimSpace(string(b))
	switch text {
	case "null":
		*v = false
		return nil
	case "true":
		*v = true
		return nil
	case "false":
		*v = false
		return nil
	}
	n, err := strconv.ParseFloat(strings.Trim(text, `"`), 64)
	if err != nil {
		return err
	}
	*v = n != 0
	return nil
}

// Project is one Todoist project.
type Project struct {
	ID             ID     `json:"id"`
	Name           string `json:"name"`
	Color          string `json:"color"`
	ParentID       ID     `json:"parent_id"`
	ChildOrder     int    `json:"child_order"`
	InboxProject   Bool   `json:"inbox_project"`
	IsInboxProject Bool   `json:"is_inbox_project"`
	IsDeleted      Bool   `json:"is_deleted"`
	IsArchived     Bool   `json:"is_archived"`
}

// IsInbox reports whether this project is the account inbox. The field name
// changed between Sync v9 and API v1, so both spellings count.
func (p Project) IsInbox() bool { return bool(p.InboxProject) || bool(p.IsInboxProject) }

// Label is one Todoist personal label.
type Label struct {
	ID        ID     `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	ItemOrder int    `json:"item_order"`
	IsDeleted Bool   `json:"is_deleted"`
}

// Section groups tasks inside a project.
type Section struct {
	ID           ID     `json:"id"`
	Name         string `json:"name"`
	ProjectID    ID     `json:"project_id"`
	SectionOrder int    `json:"section_order"`
	IsDeleted    Bool   `json:"is_deleted"`
	IsArchived   Bool   `json:"is_archived"`
}

// Due is the due date of a task. Date is either "2006-01-02" or a timestamp.
// String keeps the words that the user typed, for example "every 2 weeks".
type Due struct {
	Date        string `json:"date"`
	Timezone    string `json:"timezone"`
	String      string `json:"string"`
	Lang        string `json:"lang"`
	IsRecurring Bool   `json:"is_recurring"`
}

// Deadline is the separate hard date of a task.
type Deadline struct {
	Date string `json:"date"`
}

// Duration is the planned length of a task.
type Duration struct {
	Amount int    `json:"amount"`
	Unit   string `json:"unit"`
}

// Minutes returns the duration in minutes, or zero when it is not usable.
func (d *Duration) Minutes() int {
	if d == nil || d.Amount <= 0 {
		return 0
	}
	switch d.Unit {
	case "minute", "minutes", "":
		return d.Amount
	case "day", "days":
		return d.Amount * 24 * 60
	default:
		return 0
	}
}

// Item is one Todoist task.
type Item struct {
	ID          ID        `json:"id"`
	ProjectID   ID        `json:"project_id"`
	ParentID    ID        `json:"parent_id"`
	SectionID   ID        `json:"section_id"`
	Content     string    `json:"content"`
	Description string    `json:"description"`
	Priority    int       `json:"priority"`
	ChildOrder  int       `json:"child_order"`
	Labels      []string  `json:"labels"`
	Due         *Due      `json:"due"`
	Deadline    *Deadline `json:"deadline"`
	Duration    *Duration `json:"duration"`
	Checked     Bool      `json:"checked"`
	IsDeleted   Bool      `json:"is_deleted"`
	AddedAt     string    `json:"added_at"`
	CompletedAt string    `json:"completed_at"`
}

// Note is one comment. A task comment carries an item id; a project comment
// carries only a project id.
type Note struct {
	ID        ID     `json:"id"`
	ItemID    ID     `json:"item_id"`
	TaskID    ID     `json:"task_id"`
	ProjectID ID     `json:"project_id"`
	Content   string `json:"content"`
	PostedAt  string `json:"posted_at"`
	IsDeleted Bool   `json:"is_deleted"`
}

// Task returns the id of the task that a comment belongs to. API v1 renamed
// the field from item_id to task_id, so both spellings count.
func (n Note) Task() ID {
	if n.ItemID != "" {
		return n.ItemID
	}
	return n.TaskID
}

// CompletedInfo counts the archived tasks of a project. The sync payload
// carries the counts only, never the archived tasks themselves.
type CompletedInfo struct {
	ProjectID      ID  `json:"project_id"`
	SectionID      ID  `json:"section_id"`
	ItemID         ID  `json:"item_id"`
	CompletedItems int `json:"completed_items"`
}

// Sync is the answer of the sync endpoint.
type Sync struct {
	SyncToken     string          `json:"sync_token"`
	FullSync      Bool            `json:"full_sync"`
	Projects      []Project       `json:"projects"`
	Items         []Item          `json:"items"`
	Labels        []Label         `json:"labels"`
	Sections      []Section       `json:"sections"`
	Notes         []Note          `json:"notes"`
	ProjectNotes  []Note          `json:"project_notes"`
	CompletedInfo []CompletedInfo `json:"completed_info"`
	// NextCursor and HasMore appear on the paged reads of API v1. A plain full
	// sync leaves them empty.
	NextCursor string `json:"next_cursor"`
	HasMore    Bool   `json:"has_more"`
}

// merge appends a later page onto the first one.
func (s *Sync) merge(next *Sync) {
	s.Projects = append(s.Projects, next.Projects...)
	s.Items = append(s.Items, next.Items...)
	s.Labels = append(s.Labels, next.Labels...)
	s.Sections = append(s.Sections, next.Sections...)
	s.Notes = append(s.Notes, next.Notes...)
	s.ProjectNotes = append(s.ProjectNotes, next.ProjectNotes...)
	s.CompletedInfo = append(s.CompletedInfo, next.CompletedInfo...)
	if next.SyncToken != "" {
		s.SyncToken = next.SyncToken
	}
	s.NextCursor = next.NextCursor
	s.HasMore = next.HasMore
}
