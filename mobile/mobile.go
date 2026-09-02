// SPDX-License-Identifier: Apache-2.0

// Package mobile is the gomobile binding for the shared packages.
//
// D-002 in docs/DECISIONS.md says that every client uses one parser, one filter
// compiler, one recurrence engine and one identifier scheme. `gomobile bind`
// turns this package into an .aar for Android and an .xcframework for iOS, so
// both platforms link the same code that the server runs.
//
// Every function takes and returns strings, and every compound value is JSON.
// That is a restriction of gomobile, not a preference: the binding supports
// only strings, numbers, booleans, []byte and types declared in the bound
// package. A []string or a struct with a slice field cannot cross the boundary.
//
// Every function is total. None of them returns an error value, because an
// error across the binding becomes an exception in Kotlin and a caller then
// needs a try block around a parse that cannot fail. A failure arrives inside
// the JSON instead, as an "error" key.
//
// Dates are ISO days, "2006-01-02". The caller passes today explicitly rather
// than letting this package read the clock, so that a test is stable and so
// that the phone time zone decides what "today" means.
package mobile

import (
	"encoding/json"
	"time"

	"github.com/lightheaded/teha/filter"
	"github.com/lightheaded/teha/id"
	"github.com/lightheaded/teha/quickadd"
	"github.com/lightheaded/teha/recur"
)

const isoDay = "2006-01-02"

// errJSON renders a failure in the shape every function here uses.
func errJSON(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}

// day reads an ISO day. An unreadable value is a caller bug, so it falls back
// to the zero time rather than failing: a parse still returns a title, which is
// more useful to a user than an exception.
func day(iso string) (time.Time, bool) {
	t, err := time.Parse(isoDay, iso)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ParseQuickAdd reads one quick add line and returns what it found, as JSON.
//
// todayISO fixes the meaning of a relative word such as "tomorrow".
//
// The result carries the same fields as quickadd.Result:
//
//	{"title":"Book the ferry","due":"2026-09-01","time":"09:30",
//	 "priority":1,"project":"Trip","labels":["call"],
//	 "rrule":"","remind_at":"","remind_before":0,
//	 "parsed":["next tuesday","09:30","p1","#Trip","@call"]}
//
// remind_at is a clock time on the due day, and remind_before is a count of
// minutes before the due moment. A line sets one or neither. The phone holds
// no reminder row yet, so it can show the field and cannot arm it: see
// docs/BACKLOG.md.
//
// An empty string, an empty list or a zero priority means that the line said
// nothing about that field.
func ParseQuickAdd(text, todayISO string) string {
	t, ok := day(todayISO)
	if !ok {
		return errJSON("todayISO must be an ISO day, for example 2026-08-25")
	}
	r := quickadd.Parse(text, t)
	out := struct {
		Title        string   `json:"title"`
		Due          string   `json:"due"`
		Time         string   `json:"time"`
		Priority     int      `json:"priority"`
		Project      string   `json:"project"`
		Labels       []string `json:"labels"`
		RRule        string   `json:"rrule"`
		RemindAt     string   `json:"remind_at"`
		RemindBefore int      `json:"remind_before"`
		Parsed       []string `json:"parsed"`
	}{r.Title, r.Due, r.Time, r.Priority, r.Project, r.Labels, r.RRule,
		r.RemindAt, r.RemindBefore, r.Parsed}
	if out.Labels == nil {
		out.Labels = []string{}
	}
	if out.Parsed == nil {
		out.Parsed = []string{}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return errJSON(err.Error())
	}
	return string(b)
}

// CompileFilter turns a filter query into a SQL WHERE clause and its
// arguments, with the names of the server database.
//
//	{"sql":"(due_date IS NOT NULL AND due_date <= ?) AND state = 'open'",
//	 "args":["2026-08-25"]}
//
// A query the grammar rejects returns {"error":"..."} instead. Show that text
// to the user: it names the position that failed.
//
// A client that reads its own Room database calls CompileFilterRoom instead.
func CompileFilter(query, todayISO string) string {
	return compile(query, todayISO, filter.ServerSchema)
}

// CompileFilterRoom compiles a filter against the Android database.
//
// Room holds the same rows as the server under different names: the table
// `tasks` rather than `task`, camelCase columns, and every label name of a task
// in one comma-joined column rather than in a join table. The compiler takes
// the names as a value and emits either dialect, so one filter string means one
// thing on the phone, in the browser and on the server. See filter.Schema for
// the mapping and for the reason it is not a rewrite of the finished SQL.
//
// The answer has the same shape as CompileFilter. The clause is a WHERE clause
// only, so the caller adds the test for a deleted row and the sort of the view.
//
//	{"sql":"(dueDate IS NOT NULL AND dueDate <= ?) AND state = 'open'",
//	 "args":["2026-08-25"]}
func CompileFilterRoom(query, todayISO string) string {
	return compile(query, todayISO, filter.RoomSchema)
}

// CompileFilterRoomFor compiles a filter against the Android database for one
// account.
//
// meID is the account that is asking, and `assigned to: me` is the term that
// needs it. A phone that has not joined a household passes an empty string,
// and the term then fails with a sentence rather than answering for the wrong
// person.
func CompileFilterRoomFor(query, todayISO, meID string) string {
	schema := filter.RoomSchema
	schema.Me = meID
	return compile(query, todayISO, schema)
}

func compile(query, todayISO string, schema filter.Schema) string {
	t, ok := day(todayISO)
	if !ok {
		return errJSON("todayISO must be an ISO day, for example 2026-08-25")
	}
	sql, args, err := filter.CompileFor(query, t, schema)
	if err != nil {
		return errJSON(err.Error())
	}
	if args == nil {
		args = []any{}
	}
	b, err := json.Marshal(struct {
		SQL  string `json:"sql"`
		Args []any  `json:"args"`
	}{sql, args})
	if err != nil {
		return errJSON(err.Error())
	}
	return string(b)
}

// NextRecurrence returns the next due day for a repeating task.
//
// rule is an RRULE. base is the current due day. today is the day the user
// completed the task. fromCompletion is true for a Todoist "every!" rule, which
// counts from the completion rather than from the due day.
//
//	{"due":"2026-09-08"}
//
// A rule that does not parse returns {"error":"..."}.
func NextRecurrence(rule, base, today string, fromCompletion bool) string {
	next, err := recur.Next(rule, base, today, fromCompletion)
	if err != nil {
		return errJSON(err.Error())
	}
	b, _ := json.Marshal(map[string]string{"due": next})
	return string(b)
}

// ValidRecurrence reports whether an RRULE parses.
//
//	{"valid":true}  or  {"valid":false,"error":"..."}
func ValidRecurrence(rule string) string {
	if err := recur.Valid(rule); err != nil {
		b, _ := json.Marshal(struct {
			Valid bool   `json:"valid"`
			Error string `json:"error"`
		}{false, err.Error()})
		return string(b)
	}
	b, _ := json.Marshal(map[string]bool{"valid": true})
	return string(b)
}

// NewID returns a new short sortable identifier.
//
// The client generates the identifier, not the server, because an offline edit
// must name its own row. The scheme carries a counter inside the millisecond,
// so a bulk import stays unique. See package id.
//
// prefix is a short row-type marker, for example "t" for a task.
func NewID(prefix string) string {
	return id.New(prefix)
}
