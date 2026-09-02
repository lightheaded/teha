// SPDX-License-Identifier: AGPL-3.0-or-later

// Package mcpsrv exposes the store to an AI agent over MCP.
//
// The design target is token cost and round trips, not feature symmetry with
// the app. Rules:
//
//   - Few tools. Ten, not fifty.
//   - Every mutation takes a batch, so twenty edits cost one call.
//   - Every list runs a server-side filter in SQL, so the model never pulls a
//     project to loop over it.
//   - Output uses short keys and drops empty fields. A typical list of 50
//     tasks stays near 1 500 tokens.
//
// The transport is Streamable HTTP from specification revision 2026-07-28,
// which is stateless: no session header, and no server-initiated request.
package mcpsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lightheaded/teha/id"

	"github.com/lightheaded/teha/filter"
	"github.com/lightheaded/teha/internal/api"
	"github.com/lightheaded/teha/internal/store"
	"github.com/lightheaded/teha/recur"
)

const filterHelp = `A filter is a query string. Terms: today, tomorrow, overdue, no date, ` +
	`recurring, subtask, done, started, deferred, p1..p4, no priority, deadline, no deadline, ` +
	`#Project, ##Project (with sub-projects), /Section, no section, %label, search: text, ` +
	`before: <date>, after: <date>. ` +
	`Operators: & (and), | (or), ! (not), parentheses. Example: "overdue | today & #Home & !%errand".`

// Handler builds the MCP HTTP handler.
type Handler struct {
	Store *store.Store
	API   *api.Server
	Now   func() time.Time

	mu      sync.Mutex
	servers map[string]*mcp.Server
}

// view is the handler bound to one account. Every tool answers for the person
// whose token drove the call, so an agent of one person never reads or writes
// the other person's lists.
type view struct {
	*Handler
	me store.Account
}

// New builds the handler.
func New(s *store.Store, a *api.Server) *Handler {
	return &Handler{Store: s, API: a, Now: time.Now, servers: map[string]*mcp.Server{}}
}

// HTTP returns the handler to mount at /mcp.
//
// Stateless is on, which is the model of specification revision 2026-07-28:
// no session header, and every POST carries its own context. JSONResponse is
// on because a tool result is one small object, so a stream frame adds cost
// without adding value.
func (h *Handler) HTTP() http.Handler {
	// One server per account, built once and kept. The tools of an account do
	// not change at runtime, and a household holds two or three people, so the
	// map stays tiny.
	return mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			me := store.Account{ID: store.OwnerID, InboxID: store.InboxID}
			if h.API != nil {
				if a, ok := h.API.Caller(r); ok {
					me = a
				}
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			if srv, ok := h.servers[me.ID]; ok {
				return srv
			}
			srv := h.Server(me)
			h.servers[me.ID] = srv
			return srv
		},
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
}

// Server builds the MCP server with every tool registered.
func (h *Handler) Server(me store.Account) *mcp.Server {
	if me.ID == "" {
		me = store.Account{ID: store.OwnerID, InboxID: store.InboxID}
	}
	if me.InboxID == "" {
		me.InboxID = store.InboxID
	}
	h2 := &view{Handler: h, me: me}
	s := mcp.NewServer(&mcp.Implementation{Name: "teha", Version: "0.1.0"}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_tasks",
		Description: "List tasks that match a filter. This is the main read tool: " +
			"push the work into the filter instead of listing a project and looping. " + filterHelp,
	}, h2.listTasks)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_tasks",
		Description: "Add one or many tasks in one call. Dates are YYYY-MM-DD. Repeat rules are RRULE strings, for example FREQ=WEEKLY;BYDAY=MO.",
	}, h2.addTasks)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_tasks",
		Description: "Change one or many tasks in one call. Send only the fields that change. Use clear to empty a field.",
	}, h2.updateTasks)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "complete_tasks",
		Description: "Complete one or many tasks by id. A repeating task moves to its next date instead of closing.",
	}, h2.completeTasks)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_projects",
		Description: "List every project with its id, name, open task count and sections. A section is a heading inside a project, and the filter term for one is /Name.",
	}, h2.listProjects)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_project",
		Description: "Create a project.",
	}, h2.addProject)

	mcp.AddTool(s, &mcp.Tool{
		Name: "comments",
		Description: "Read the comments on one task, oldest first. A comment is the talk about " +
			"a task, and the filter term for the text of one is comment: words.",
	}, h2.comments)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_comment",
		Description: "Leave a comment on a task. Use it to record what you found, so the next reader has it.",
	}, h2.addComment)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Full-text search over task titles and descriptions.",
	}, h2.search)

	mcp.AddTool(s, &mcp.Tool{
		Name: "plan_day",
		Description: "One call for the daily ritual: overdue tasks, tasks due today, and the " +
			"undated pile grouped by project. Use this instead of three list calls.",
	}, h2.planDay)

	return s
}

// --- compact wire shapes ----------------------------------------------------

// taskOut uses short keys on purpose. Every empty field is dropped.
type taskOut struct {
	ID    string   `json:"id"`
	Ti    string   `json:"ti"`            // title
	Due   string   `json:"due,omitempty"` // YYYY-MM-DD
	Time  string   `json:"tm,omitempty"`
	P     int      `json:"p,omitempty"`   // priority, 1 is highest, 4 is dropped
	Pr    string   `json:"pr,omitempty"`  // project name
	Lb    []string `json:"lb,omitempty"`  // labels
	Rec   string   `json:"rec,omitempty"` // rrule
	Sub   bool     `json:"sub,omitempty"` // has a parent
	Desc  string   `json:"d,omitempty"`
	State string   `json:"st,omitempty"` // only when not open
}

func (h *view) toOut(t store.Task, names map[string]string, fields map[string]bool) taskOut {
	o := taskOut{ID: t.ID, Ti: t.Title}
	if t.DueDate != nil {
		o.Due = *t.DueDate
	}
	if t.DueTime != nil {
		o.Time = *t.DueTime
	}
	if t.Priority != 4 {
		o.P = t.Priority
	}
	if n, ok := names[t.ProjectID]; ok && t.ProjectID != store.InboxID {
		o.Pr = n
	}
	o.Lb = t.Labels
	if t.RRule != nil {
		o.Rec = *t.RRule
	}
	o.Sub = t.ParentID != nil
	if t.State != "open" {
		o.State = t.State
	}
	if fields["description"] && t.Description != "" {
		o.Desc = t.Description
	}
	return o
}

func (h *view) projectNames() (map[string]string, error) {
	ps, err := h.Store.ProjectsFor(h.me.ID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(ps))
	for _, p := range ps {
		out[p.ID] = p.Name
	}
	return out, nil
}

func fieldSet(list []string) map[string]bool {
	m := map[string]bool{}
	for _, f := range list {
		m[strings.ToLower(strings.TrimSpace(f))] = true
	}
	return m
}

// --- tools ------------------------------------------------------------------

type listTasksArgs struct {
	Filter string   `json:"filter" jsonschema:"the filter query. An empty filter means every open task"`
	Fields []string `json:"fields,omitempty" jsonschema:"extra fields to include. Only description is supported today"`
	Limit  int      `json:"limit,omitempty" jsonschema:"maximum rows, default 50, maximum 200"`
	Cursor int      `json:"cursor,omitempty" jsonschema:"row offset from a previous call, for the next page"`
}

type listTasksOut struct {
	T    []taskOut `json:"t"`
	N    int       `json:"n"`
	Next int       `json:"next,omitempty"`
}

func (h *view) listTasks(ctx context.Context, req *mcp.CallToolRequest, a listTasksArgs) (*mcp.CallToolResult, any, error) {
	where, args, err := filter.CompileFor(a.Filter, h.Now(), h.schema())
	if err != nil {
		return toolError("the filter did not parse: " + err.Error() + " " + filterHelp)
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	tasks, err := h.Store.QueryFor(where, args, limit+1, a.Cursor, h.me.ID)
	if err != nil {
		return nil, nil, err
	}
	names, err := h.projectNames()
	if err != nil {
		return nil, nil, err
	}
	fs := fieldSet(a.Fields)
	out := listTasksOut{T: []taskOut{}}
	for i, t := range tasks {
		if i == limit {
			out.Next = a.Cursor + limit
			break
		}
		out.T = append(out.T, h.toOut(t, names, fs))
	}
	out.N = len(out.T)
	return structured(out)
}

type addTaskArg struct {
	Title       string   `json:"title"`
	Project     string   `json:"project,omitempty" jsonschema:"project name. Empty means the inbox"`
	Due         string   `json:"due,omitempty" jsonschema:"YYYY-MM-DD"`
	Time        string   `json:"time,omitempty" jsonschema:"HH:MM"`
	Priority    int      `json:"priority,omitempty" jsonschema:"1 is highest, 4 is none"`
	Labels      []string `json:"labels,omitempty"`
	Repeat      string   `json:"repeat,omitempty" jsonschema:"an RRULE string, for example FREQ=DAILY"`
	Description string   `json:"description,omitempty"`
	StartDate   string   `json:"start_date,omitempty" jsonschema:"the task stays hidden from today until this date"`
	Deadline    string   `json:"deadline,omitempty"`
	ParentID    string   `json:"parent_id,omitempty" jsonschema:"make this task a sub-task of that id"`
}

type addTasksArgs struct {
	Tasks []addTaskArg `json:"tasks" jsonschema:"one or many tasks to add"`
}

type writeOut struct {
	OK     int      `json:"ok"`
	IDs    []string `json:"ids,omitempty"`
	Errors []string `json:"errors,omitempty"`
	V      int64    `json:"v"`
}

func (h *view) addTasks(ctx context.Context, req *mcp.CallToolRequest, a addTasksArgs) (*mcp.CallToolResult, any, error) {
	if len(a.Tasks) == 0 {
		return toolError("add_tasks needs at least one task")
	}
	cmds := make([]store.Command, 0, len(a.Tasks))
	for _, t := range a.Tasks {
		if strings.TrimSpace(t.Title) == "" {
			return toolError("every task needs a title")
		}
		if t.Repeat != "" {
			if err := recur.Valid(t.Repeat); err != nil {
				return toolError(err.Error())
			}
		}
		args := store.TaskArgs{ID: id.New("t"), Title: ptr(t.Title)}
		if t.Project != "" {
			args.Project = ptr(t.Project)
		}
		if t.Due != "" {
			args.DueDate = ptr(t.Due)
		}
		if t.Time != "" {
			args.DueTime = ptr(t.Time)
		}
		if t.Priority != 0 {
			args.Priority = ptr(t.Priority)
		}
		if t.Repeat != "" {
			args.RRule = ptr(t.Repeat)
		}
		if t.Description != "" {
			args.Description = ptr(t.Description)
		}
		if t.StartDate != "" {
			args.StartDate = ptr(t.StartDate)
		}
		if t.Deadline != "" {
			args.Deadline = ptr(t.Deadline)
		}
		if t.ParentID != "" {
			args.ParentID = ptr(t.ParentID)
		}
		args.Labels = t.Labels
		cmds = append(cmds, command("task_add", args))
	}
	return h.run(cmds)
}

type updateTaskArg struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Project     string   `json:"project,omitempty"`
	Due         string   `json:"due,omitempty"`
	Time        string   `json:"time,omitempty"`
	Priority    int      `json:"priority,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Repeat      string   `json:"repeat,omitempty"`
	Description string   `json:"description,omitempty"`
	StartDate   string   `json:"start_date,omitempty"`
	Deadline    string   `json:"deadline,omitempty"`
	Clear       []string `json:"clear,omitempty" jsonschema:"field names to empty: due_date, due_time, rrule, start_date, deadline, parent_id"`
}

type updateTasksArgs struct {
	Tasks []updateTaskArg `json:"tasks"`
}

func (h *view) updateTasks(ctx context.Context, req *mcp.CallToolRequest, a updateTasksArgs) (*mcp.CallToolResult, any, error) {
	if len(a.Tasks) == 0 {
		return toolError("update_tasks needs at least one task")
	}
	cmds := make([]store.Command, 0, len(a.Tasks))
	for _, t := range a.Tasks {
		if t.ID == "" {
			return toolError("every entry needs an id")
		}
		args := store.TaskArgs{ID: t.ID, Clear: t.Clear}
		if t.Title != "" {
			args.Title = ptr(t.Title)
		}
		if t.Project != "" {
			args.Project = ptr(t.Project)
		}
		if t.Due != "" {
			args.DueDate = ptr(t.Due)
		}
		if t.Time != "" {
			args.DueTime = ptr(t.Time)
		}
		if t.Priority != 0 {
			args.Priority = ptr(t.Priority)
		}
		if t.Repeat != "" {
			if err := recur.Valid(t.Repeat); err != nil {
				return toolError(err.Error())
			}
			args.RRule = ptr(t.Repeat)
		}
		if t.Description != "" {
			args.Description = ptr(t.Description)
		}
		if t.StartDate != "" {
			args.StartDate = ptr(t.StartDate)
		}
		if t.Deadline != "" {
			args.Deadline = ptr(t.Deadline)
		}
		if t.Labels != nil {
			args.Labels = t.Labels
		}
		cmds = append(cmds, command("task_update", args))
	}
	return h.run(cmds)
}

type completeArgs struct {
	IDs    []string `json:"ids"`
	WontDo bool     `json:"wont_do,omitempty" jsonschema:"close the task as will not do instead of done"`
}

func (h *view) completeTasks(ctx context.Context, req *mcp.CallToolRequest, a completeArgs) (*mcp.CallToolResult, any, error) {
	if len(a.IDs) == 0 {
		return toolError("complete_tasks needs at least one id")
	}
	kind := "task_complete"
	if a.WontDo {
		kind = "task_wont_do"
	}
	cmds := make([]store.Command, 0, len(a.IDs))
	for _, id := range a.IDs {
		cmds = append(cmds, command(kind, store.IDArgs{ID: id}))
	}
	return h.run(cmds)
}

type projectOut struct {
	ID   string       `json:"id"`
	Name string       `json:"name"`
	Open int          `json:"open"`
	Sec  []sectionOut `json:"sec,omitempty"`
}

// sectionOut carries the id and the name only. A per-section open count would
// almost double the cost of this answer, and the model can get the same number
// from list_tasks with a /Name term when it needs it.
type sectionOut struct {
	ID   string `json:"id"`
	Name string `json:"n"`
}

func (h *view) listProjects(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	ps, err := h.Store.ProjectsFor(h.me.ID)
	if err != nil {
		return nil, nil, err
	}
	secs, err := h.Store.SectionsFor(h.me.ID)
	if err != nil {
		return nil, nil, err
	}
	byProject := map[string][]sectionOut{}
	for _, sec := range secs {
		byProject[sec.ProjectID] = append(byProject[sec.ProjectID], sectionOut{ID: sec.ID, Name: sec.Name})
	}
	out := make([]projectOut, 0, len(ps))
	for _, p := range ps {
		tasks, err := h.Store.QueryFor("project_id = ?", []any{p.ID}, 500, 0, h.me.ID)
		if err != nil {
			return nil, nil, err
		}
		open := 0
		for _, t := range tasks {
			if t.State == "open" {
				open++
			}
		}
		out = append(out, projectOut{ID: p.ID, Name: p.Name, Open: open, Sec: byProject[p.ID]})
	}
	return structured(map[string]any{"projects": out})
}

type addProjectArgs struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

func (h *view) addProject(ctx context.Context, req *mcp.CallToolRequest, a addProjectArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(a.Name) == "" {
		return toolError("a project needs a name")
	}
	args := store.ProjectArgs{ID: id.New("p"), Name: ptr(a.Name)}
	if a.Color != "" {
		args.Color = ptr(a.Color)
	}
	return h.run([]store.Command{command("project_add", args)})
}

type commentsArgs struct {
	TaskID string `json:"task_id"`
}

// commentOut uses short keys, like taskOut. Wh is who wrote the line, as a
// name, because an account id means nothing to a reader.
type commentOut struct {
	ID   string `json:"id"`
	Wh   string `json:"wh"`
	At   string `json:"at"`
	Body string `json:"body"`
}

type commentsOut struct {
	Task string       `json:"task"`
	C    []commentOut `json:"c"`
	N    int          `json:"n"`
}

func (h *view) comments(ctx context.Context, req *mcp.CallToolRequest, a commentsArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(a.TaskID) == "" {
		return toolError("comments needs a task_id")
	}
	rows, err := h.Store.CommentsFor(a.TaskID, h.me.ID)
	if err != nil {
		return toolError("no task with id " + a.TaskID)
	}
	who, err := h.peopleNames()
	if err != nil {
		return nil, nil, err
	}
	out := commentsOut{Task: a.TaskID, C: []commentOut{}}
	for _, c := range rows {
		name := who[c.AccountID]
		if name == "" {
			name = c.AccountID
		}
		out.C = append(out.C, commentOut{ID: c.ID, Wh: name, At: c.CreatedAt, Body: c.Body})
	}
	out.N = len(out.C)
	return structured(out)
}

type addCommentArgs struct {
	TaskID string `json:"task_id"`
	Body   string `json:"body"`
}

func (h *view) addComment(ctx context.Context, req *mcp.CallToolRequest, a addCommentArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(a.TaskID) == "" || strings.TrimSpace(a.Body) == "" {
		return toolError("a comment needs a task_id and a body")
	}
	return h.run([]store.Command{command("comment_add", store.CommentArgs{
		ID: id.New("cm"), TaskID: ptr(a.TaskID), Body: ptr(a.Body),
	})})
}

// peopleNames maps an account id to the name a person reads.
func (h *view) peopleNames() (map[string]string, error) {
	people, err := h.Store.Accounts()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(people))
	for _, p := range people {
		name := p.DisplayName
		if name == "" {
			name = p.Name
		}
		out[p.ID] = name
	}
	return out, nil
}

type searchArgs struct {
	Text  string `json:"text"`
	Limit int    `json:"limit,omitempty"`
}

func (h *view) search(ctx context.Context, req *mcp.CallToolRequest, a searchArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(a.Text) == "" {
		return toolError("search needs text")
	}
	ids, err := h.Store.SearchFor(a.Text, a.Limit, h.me.ID)
	if err != nil {
		return toolError("the search index rejected that text: " + err.Error())
	}
	names, err := h.projectNames()
	if err != nil {
		return nil, nil, err
	}
	out := listTasksOut{T: []taskOut{}}
	for _, id := range ids {
		t, err := h.Store.TaskFor(id, h.me.ID)
		if err != nil {
			continue
		}
		if t.DeletedAt != nil {
			continue
		}
		out.T = append(out.T, h.toOut(t, names, nil))
	}
	out.N = len(out.T)
	return structured(out)
}

type planDayOut struct {
	Today   string              `json:"today"`
	Overdue []taskOut           `json:"overdue"`
	Due     []taskOut           `json:"due"`
	Undated map[string][]string `json:"undated,omitempty"`
	Counts  map[string]int      `json:"n"`
}

func (h *view) planDay(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	now := h.Now()
	names, err := h.projectNames()
	if err != nil {
		return nil, nil, err
	}
	get := func(q string, limit int) ([]store.Task, error) {
		where, args, err := filter.CompileFor(q, now, h.schema())
		if err != nil {
			return nil, err
		}
		return h.Store.QueryFor(where, args, limit, 0, h.me.ID)
	}
	overdue, err := get("overdue", 100)
	if err != nil {
		return nil, nil, err
	}
	due, err := get("today & !overdue", 100)
	if err != nil {
		return nil, nil, err
	}
	undated, err := get("no date", 200)
	if err != nil {
		return nil, nil, err
	}

	out := planDayOut{Today: now.Format("2006-01-02"), Overdue: []taskOut{}, Due: []taskOut{},
		Undated: map[string][]string{}, Counts: map[string]int{}}
	for _, t := range overdue {
		out.Overdue = append(out.Overdue, h.toOut(t, names, nil))
	}
	for _, t := range due {
		out.Due = append(out.Due, h.toOut(t, names, nil))
	}
	for _, t := range undated {
		name := names[t.ProjectID]
		if name == "" {
			name = "Inbox"
		}
		out.Undated[name] = append(out.Undated[name], t.Title)
	}
	out.Counts["overdue"] = len(out.Overdue)
	out.Counts["due"] = len(out.Due)
	out.Counts["undated"] = len(undated)
	return structured(out)
}

// --- plumbing ---------------------------------------------------------------

// schema names the account that is asking, so `#inbox` reaches that person's
// inbox and `assigned to: me` reaches that person.
func (h *view) schema() filter.Schema {
	sc := filter.ServerSchema
	sc.Me = h.me.ID
	sc.InboxID = h.me.InboxID
	return sc
}

func (h *view) run(cmds []store.Command) (*mcp.CallToolResult, any, error) {
	v, results, events, err := h.Store.ApplyWithEvents(cmds, h.me.ID)
	if err != nil {
		return nil, nil, err
	}
	out := writeOut{V: v}
	for _, r := range results {
		if r.OK {
			out.OK++
			if r.ID != "" {
				out.IDs = append(out.IDs, r.ID)
			}
		} else {
			out.Errors = append(out.Errors, r.Error)
		}
	}
	if h.API != nil {
		h.API.Notify(v)
		// An agent that gives somebody a task, or leaves a comment, tells them
		// the same way a person does.
		h.API.Deliver(events)
	}
	return structured(out)
}

func command(kind string, args any) store.Command {
	raw, _ := json.Marshal(args)
	return store.Command{UUID: uuid.NewString(), Type: kind, Args: raw}
}

// structured returns compact JSON as the tool result. The text content holds
// the same JSON, because some clients read only the text.
func structured(v any) (*mcp.CallToolResult, any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
	}, v, nil
}

func toolError(msg string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

func ptr[T any](v T) *T { return &v }

var _ = fmt.Sprintf
