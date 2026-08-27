// SPDX-License-Identifier: Apache-2.0

package filter

// Schema names the tables and the columns that a compiled filter reads.
//
// Two stores hold the same rows under different names. The server keeps a task
// in `task` with snake_case columns and a `task_label` join table. The Android
// client keeps the same task in a Room table `tasks` with camelCase columns and
// every label name in one comma-joined column. One filter string must mean the
// same thing in both places, so the compiler takes the names as a value and
// emits either dialect from one parser.
//
// The mapping lives inside the compiler, and not in a rewrite of the finished
// SQL. A rewrite has to find a column name inside a string that also holds user
// text, so `search: due_date` loses its search term. A name that the compiler
// never wrote cannot be mangled.
//
// A Schema is a plain value. The filter package therefore stays free of any
// dependency on either store, and the Apache-2.0 layer never imports server
// code. See D-001 in docs/DECISIONS.md.
type Schema struct {
	// The tables.
	Task      string
	Project   string
	Label     string
	TaskLabel string
	// Section is empty on a store that holds no section table. A `/section`
	// term then fails with a message that says so.
	Section string

	// Columns that every table above spells the same way.
	ID        string
	Name      string
	ParentID  string
	DeletedAt string

	// The columns of the task table.
	ProjectID   string
	Title       string
	Description string
	Priority    string
	DueDate     string
	DueTime     string
	RRule       string
	StartDate   string
	Deadline    string
	State       string
	// SectionID is empty on a store that keeps no section on a task. It goes
	// with Section: both are set, or neither is.
	SectionID string
	// CreatedAt is empty on a store that keeps no creation date. A `created:`
	// term then fails with a message that says so, rather than compiling to
	// SQL that names a column the store does not have.
	CreatedAt string

	// The columns of the join table.
	TaskLabelTask  string
	TaskLabelLabel string

	// Labels is the task column that holds every label name of the task,
	// joined by a comma. When it is empty, the labels of a task live in the
	// TaskLabel and Label tables instead.
	Labels string

	// InboxID is the row id of the inbox project. The server fixes it, so a
	// client can name the inbox without a lookup. See store.InboxID.
	InboxID string
}

// ServerSchema holds the names of internal/store/schema.sql.
var ServerSchema = Schema{
	Task:      "task",
	Project:   "project",
	Label:     "label",
	TaskLabel: "task_label",
	Section:   "section",

	ID:        "id",
	Name:      "name",
	ParentID:  "parent_id",
	DeletedAt: "deleted_at",

	ProjectID:   "project_id",
	Title:       "title",
	Description: "description",
	Priority:    "priority",
	DueDate:     "due_date",
	DueTime:     "due_time",
	RRule:       "rrule",
	StartDate:   "start_date",
	Deadline:    "deadline",
	State:       "state",
	SectionID:   "section_id",
	CreatedAt:   "created_at",

	TaskLabelTask:  "task_id",
	TaskLabelLabel: "label_id",

	InboxID: "inbox",
}

// RoomSchema holds the names of the Android database, which the Room entities in
// android/app/src/main/kotlin/io/github/lightheaded/teha/data/db/Entities.kt
// declare.
//
// Three differences carry a consequence, and each one has its answer in
// Compile:
//
//   - Labels is a comma-joined column, so a label term becomes a LIKE over a
//     padded copy of that column and never a join.
//   - CreatedAt is absent, so a `created:` term fails with a message. Section
//     and SectionID are absent for the same reason, because the phone holds no
//     section table yet.
//   - The Room database holds no fts5 table, so `search:` stays a LIKE over the
//     title and the description. The server has `task_fts`, and the filter
//     compiler never names it, so both dialects search the same way.
var RoomSchema = Schema{
	Task:    "tasks",
	Project: "projects",
	Label:   "labels",

	ID:        "id",
	Name:      "name",
	ParentID:  "parentId",
	DeletedAt: "deletedAt",

	ProjectID:   "projectId",
	Title:       "title",
	Description: "description",
	Priority:    "priority",
	DueDate:     "dueDate",
	DueTime:     "dueTime",
	RRule:       "rrule",
	StartDate:   "startDate",
	Deadline:    "deadline",
	State:       "state",
	CreatedAt:   "",

	Labels: "labels",

	InboxID: "inbox",
}
