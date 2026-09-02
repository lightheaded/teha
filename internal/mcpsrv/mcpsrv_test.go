// SPDX-License-Identifier: AGPL-3.0-or-later

package mcpsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lightheaded/teha/internal/store"
)

var fixedNow = time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)

// session starts the MCP server in this process and connects a client to it.
func session(t *testing.T) (*mcp.ClientSession, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	st.Now = func() time.Time { return fixedNow }
	t.Cleanup(func() { st.Close() })

	h := New(st, nil)
	h.Now = func() time.Time { return fixedNow }

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() {
		// An empty account is the owner, which is what a one-account file is.
		if err := h.Server(store.Account{}).Run(ctx, serverTransport); err != nil {
			t.Log("server stopped:", err)
		}
	}()
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, st
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("%s returned no content", name)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if res.IsError {
		t.Fatalf("%s reported an error: %s", name, text)
	}
	return text
}

func TestToolListIsSmallAndStable(t *testing.T) {
	cs, _ := session(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) > 12 {
		t.Fatalf("%d tools: a model pays for every schema, keep the set small", len(res.Tools))
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" {
			t.Errorf("tool %s has no description", tool.Name)
		}
	}
	want := []string{"list_tasks", "add_tasks", "update_tasks", "complete_tasks", "plan_day"}
	for _, w := range want {
		if !contains(names, w) {
			t.Errorf("tool %s is missing, found %v", w, names)
		}
	}
	// A deterministic order lets a client cache the list and keeps the prompt
	// cache warm, which the 2026-07-28 revision asks for.
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("tools are not in a stable order: %v", names)
		}
	}
}

func TestAddThenListThenComplete(t *testing.T) {
	cs, _ := session(t)

	out := call(t, cs, "add_tasks", map[string]any{"tasks": []map[string]any{
		{"title": "Buy milk", "due": "2026-08-25", "priority": 1, "labels": []string{"store"}},
		{"title": "Water the plants", "repeat": "FREQ=DAILY", "due": "2026-08-25"},
		{"title": "Someday idea"},
	}})
	var added struct {
		OK  int      `json:"ok"`
		IDs []string `json:"ids"`
	}
	mustJSON(t, out, &added)
	if added.OK != 3 || len(added.IDs) != 3 {
		t.Fatalf("add_tasks returned %s", out)
	}

	list := call(t, cs, "list_tasks", map[string]any{"filter": "today"})
	var got struct {
		T []map[string]any `json:"t"`
		N int              `json:"n"`
	}
	mustJSON(t, list, &got)
	if got.N != 2 {
		t.Fatalf("today has %d tasks, want 2: %s", got.N, list)
	}
	// Compact keys and no empty fields: that is the token budget in practice.
	for _, row := range got.T {
		for k := range row {
			if len(k) > 3 && k != "state" {
				t.Errorf("key %q is long, the wire format must stay compact", k)
			}
		}
		if _, ok := row["st"]; ok {
			t.Errorf("an open task must not carry a state field: %v", row)
		}
	}

	comp := call(t, cs, "complete_tasks", map[string]any{"ids": added.IDs[:2]})
	if !strings.Contains(comp, `"ok":2`) {
		t.Fatalf("complete_tasks returned %s", comp)
	}
	// The repeating task must survive with a new date.
	rec := call(t, cs, "list_tasks", map[string]any{"filter": "recurring"})
	if !strings.Contains(rec, "2026-08-26") {
		t.Fatalf("the repeating task did not move to the next day: %s", rec)
	}
}

// An agent that reads a task has to read the talk about it, and it has to be
// able to leave what it found for the next reader.
func TestACommentIsWrittenReadAndFound(t *testing.T) {
	cs, _ := session(t)

	out := call(t, cs, "add_tasks", map[string]any{"tasks": []map[string]any{
		{"title": "Call the plumber"},
	}})
	var added struct {
		IDs []string `json:"ids"`
	}
	mustJSON(t, out, &added)
	if len(added.IDs) != 1 {
		t.Fatalf("add_tasks returned %s", out)
	}
	taskID := added.IDs[0]

	call(t, cs, "add_comment", map[string]any{"task_id": taskID, "body": "The leak is under the sink."})

	read := call(t, cs, "comments", map[string]any{"task_id": taskID})
	var got struct {
		C []struct {
			Wh   string `json:"wh"`
			Body string `json:"body"`
		} `json:"c"`
		N int `json:"n"`
	}
	mustJSON(t, read, &got)
	if got.N != 1 || got.C[0].Body != "The leak is under the sink." {
		t.Fatalf("comments returned %s", read)
	}
	if got.C[0].Wh == "" {
		t.Fatalf("a comment must name who wrote it: %s", read)
	}

	// The filter term finds the task by the text of its comment.
	found := call(t, cs, "list_tasks", map[string]any{"filter": "comment: sink"})
	if !strings.Contains(found, taskID) {
		t.Fatalf("comment: sink did not find the task: %s", found)
	}
}

func TestPlanDayIsOneCall(t *testing.T) {
	cs, _ := session(t)
	call(t, cs, "add_project", map[string]any{"name": "Home"})
	call(t, cs, "add_tasks", map[string]any{"tasks": []map[string]any{
		{"title": "Late thing", "due": "2026-08-20"},
		{"title": "Due today", "due": "2026-08-25"},
		{"title": "No date", "project": "Home"},
	}})
	out := call(t, cs, "plan_day", map[string]any{})
	var plan struct {
		Today   string              `json:"today"`
		Overdue []map[string]any    `json:"overdue"`
		Due     []map[string]any    `json:"due"`
		Undated map[string][]string `json:"undated"`
	}
	mustJSON(t, out, &plan)
	if plan.Today != "2026-08-25" || len(plan.Overdue) != 1 || len(plan.Due) != 1 {
		t.Fatalf("plan_day returned %s", out)
	}
	if got := plan.Undated["Home"]; len(got) != 1 || got[0] != "No date" {
		t.Fatalf("undated tasks are grouped wrong: %s", out)
	}
}

// The promise in the plan: a typical list call costs under 2 000 tokens. A
// typical call is the default page of 50 rows. The 200-row page is the worst
// case a model can ask for, and it must stay near 6 000 tokens, most of which
// is the task titles themselves.
func TestListStaysWithinTokenBudget(t *testing.T) {
	cs, _ := session(t)
	batch := make([]map[string]any, 0, 200)
	for i := 0; i < 200; i++ {
		batch = append(batch, map[string]any{
			"title": fmt.Sprintf("Task number %d with a realistic length title", i),
			"due":   "2026-08-25",
		})
	}
	call(t, cs, "add_tasks", map[string]any{"tasks": batch})

	typical := call(t, cs, "list_tasks", map[string]any{"filter": "today"}) // default page
	if tokens := len(typical) / 4; tokens > 2000 {
		t.Errorf("the default page costs about %d tokens, the promise is 2000", tokens)
	} else {
		t.Logf("default page of 50: %d bytes, about %d tokens", len(typical), tokens)
	}

	out := call(t, cs, "list_tasks", map[string]any{"filter": "today", "limit": 200})
	approxTokens := len(out) / 4
	if approxTokens > 6000 {
		t.Errorf("200 tasks cost about %d tokens, the ceiling is 6000", approxTokens)
	}
	var got struct {
		N    int `json:"n"`
		Next int `json:"next"`
	}
	mustJSON(t, out, &got)
	if got.N != 200 {
		t.Fatalf("got %d rows, want 200", got.N)
	}
	t.Logf("200 tasks: %d bytes, about %d tokens, %d bytes per row", len(out), approxTokens, len(out)/200)
}

func TestPagingGivesACursor(t *testing.T) {
	cs, _ := session(t)
	batch := make([]map[string]any, 0, 30)
	for i := 0; i < 30; i++ {
		batch = append(batch, map[string]any{"title": fmt.Sprintf("Item %02d", i), "due": "2026-08-25"})
	}
	call(t, cs, "add_tasks", map[string]any{"tasks": batch})

	first := call(t, cs, "list_tasks", map[string]any{"filter": "today", "limit": 10})
	var page struct {
		N    int `json:"n"`
		Next int `json:"next"`
	}
	mustJSON(t, first, &page)
	if page.N != 10 || page.Next != 10 {
		t.Fatalf("first page: %s", first)
	}
	second := call(t, cs, "list_tasks", map[string]any{"filter": "today", "limit": 10, "cursor": page.Next})
	mustJSON(t, second, &page)
	if page.N != 10 || page.Next != 20 {
		t.Fatalf("second page: %s", second)
	}
}

// A model writes a bad filter now and then. The error must teach it the
// grammar instead of failing the session.
func TestBadFilterReturnsGuidance(t *testing.T) {
	cs, _ := session(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_tasks",
		Arguments: map[string]any{"filter": "today & ("},
	})
	if err != nil {
		t.Fatalf("a bad filter must not break the call: %v", err)
	}
	if !res.IsError {
		t.Fatal("a bad filter must report a tool error")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "Terms:") {
		t.Fatalf("the error does not teach the grammar: %s", text)
	}
}

func TestUpdateAndClear(t *testing.T) {
	cs, _ := session(t)
	out := call(t, cs, "add_tasks", map[string]any{"tasks": []map[string]any{
		{"title": "Movable", "due": "2026-08-25"},
	}})
	var added struct{ IDs []string }
	mustJSON(t, out, &added)
	id := added.IDs[0]

	call(t, cs, "update_tasks", map[string]any{"tasks": []map[string]any{
		{"id": id, "title": "Moved", "priority": 2, "clear": []string{"due_date"}},
	}})
	list := call(t, cs, "list_tasks", map[string]any{"filter": "no date"})
	if !strings.Contains(list, "Moved") {
		t.Fatalf("the update did not land: %s", list)
	}
}

func mustJSON(t *testing.T, text string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(text), into); err != nil {
		t.Fatalf("cannot read the tool output %q: %v", text, err)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
