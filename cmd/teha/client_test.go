// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lightheaded/teha/internal/store"
)

// fakeServer answers the few routes the client uses and records what it got.
type fakeServer struct {
	*httptest.Server
	commands []store.Command
	tasks    []store.Task
	projects []store.Project
	filter   string
	auth     string
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{projects: []store.Project{{ID: "inbox", Name: "Inbox", IsInbox: true}}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sync", func(w http.ResponseWriter, r *http.Request) {
		f.auth = r.Header.Get("Authorization")
		var req struct {
			Since    int64           `json:"since"`
			Commands []store.Command `json:"commands"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad sync body: %v", err)
		}
		f.commands = append(f.commands, req.Commands...)
		out := map[string]any{"version": 1, "applied": []store.Result{}}
		for _, c := range req.Commands {
			out["applied"] = append(out["applied"].([]store.Result), store.Result{UUID: c.UUID, OK: true})
		}
		if req.Since == 0 {
			out["tasks"] = f.tasks
			out["projects"] = f.projects
		}
		writeTestJSON(w, out)
	})
	mux.HandleFunc("GET /v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		f.auth = r.Header.Get("Authorization")
		f.filter = r.URL.Query().Get("filter")
		writeTestJSON(w, map[string]any{"tasks": f.tasks})
	})
	mux.HandleFunc("GET /v1/projects", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, map[string]any{"projects": f.projects})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func writeTestJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// run drives the client the way the shell does and returns what it printed.
func run(t *testing.T, f *fakeServer, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runCommand(append(args, "--server", f.URL), &out)
	return out.String(), err
}

func ptr[T any](v T) *T { return &v }

func TestAddSendsOneTaskAddCommand(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "secret-value")
	f := newFakeServer(t)
	out, err := run(t, f, "add", "Book the ferry tomorrow at 9:30 p1 @call")
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if len(f.commands) != 1 || f.commands[0].Type != "task_add" {
		t.Fatalf("got %d commands, want one task_add: %+v", len(f.commands), f.commands)
	}
	var args store.TaskArgs
	if err := json.Unmarshal(f.commands[0].Args, &args); err != nil {
		t.Fatal(err)
	}
	if *args.Title != "Book the ferry" {
		t.Errorf("title: got %q", *args.Title)
	}
	if args.DueTime == nil || *args.DueTime != "09:30" {
		t.Errorf("time: got %v", args.DueTime)
	}
	if args.Priority == nil || *args.Priority != 1 {
		t.Errorf("priority: got %v", args.Priority)
	}
	if len(args.Labels) != 1 || args.Labels[0] != "call" {
		t.Errorf("labels: got %v", args.Labels)
	}
	if !strings.HasPrefix(out, "added: Book the ferry — due ") {
		t.Errorf("output: got %q", out)
	}
	if strings.Contains(out, "secret-value") {
		t.Error("the output carries the token")
	}
	if f.auth != "Bearer secret-value" {
		t.Errorf("auth header: got %q", f.auth)
	}
}

// An unknown project name must not make a project, because a typo would leave
// junk behind. The task lands in the inbox and the answer says so, so the
// capture still succeeds. The web app behaves the same way.
func TestAddSendsAnUnknownProjectToTheInbox(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	out, err := run(t, f, "add", "Pack the bags #Trip")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || f.commands[0].Type != "task_add" {
		t.Fatalf("want one task_add, got %+v", f.commands)
	}
	var args store.TaskArgs
	if err := json.Unmarshal(f.commands[0].Args, &args); err != nil {
		t.Fatal(err)
	}
	if args.ProjectID == nil || *args.ProjectID != store.InboxID {
		t.Fatalf("the task did not go to the inbox: %+v", args.ProjectID)
	}
	if !strings.Contains(out, "no project matches #Trip") {
		t.Errorf("the answer does not explain where the task went: %q", out)
	}
}

func TestAddUsesAPrefixMatchForAProject(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	f.projects = append(f.projects, store.Project{ID: "p1", Name: "Trip to Setomaa"})
	if _, err := run(t, f, "add", "Pack the bags #Trip"); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 {
		t.Fatalf("want one command, got %+v", f.commands)
	}
	var args store.TaskArgs
	_ = json.Unmarshal(f.commands[0].Args, &args)
	if *args.ProjectID != "p1" {
		t.Errorf("project: got %q, want p1", *args.ProjectID)
	}
}

func TestAddRefusesAnUnclearProjectName(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	f.projects = append(f.projects,
		store.Project{ID: "p1", Name: "Trip to Setomaa"},
		store.Project{ID: "p2", Name: "Trips home"})
	_, err := run(t, f, "add", "Pack the bags #Trip")
	if err == nil {
		t.Fatal("want an error for an unclear name")
	}
	if len(f.commands) != 0 {
		t.Errorf("nothing must be written: %+v", f.commands)
	}
}

func TestAddNeedsALine(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	if _, err := run(t, f, "add"); err == nil {
		t.Fatal("want an error for an empty line")
	}
}

func TestTodayListsWithoutColor(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	f.projects = append(f.projects, store.Project{ID: "p1", Name: "Home"})
	f.tasks = []store.Task{
		{ID: "t_1", Title: "Call the plumber", Priority: 1, ProjectID: "p1",
			DueDate: ptr("2020-01-02"), State: "open", Labels: []string{"call"}},
		{ID: "t_2", Title: "Buy milk", Priority: 4, ProjectID: "inbox", State: "open"},
	}
	out, err := run(t, f, "today")
	if err != nil {
		t.Fatal(err)
	}
	if f.filter != "today" {
		t.Errorf("filter: got %q, want today", f.filter)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("a buffer is not a terminal, so the output must carry no color: %q", out)
	}
	for _, want := range []string{"t_1", "!!!", "2 Jan 2020", "Call the plumber", "#Home", "@call", "Buy milk"} {
		if !strings.Contains(out, want) {
			t.Errorf("output has no %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "#Inbox") {
		t.Error("the inbox is the default, so it stays out of the list")
	}
}

func TestLsPassesTheFilterThrough(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	if _, err := run(t, f, "ls", "overdue | today"); err != nil {
		t.Fatal(err)
	}
	if f.filter != "overdue | today" {
		t.Errorf("filter: got %q", f.filter)
	}
}

func TestDoneCompletesOneMatch(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	f.tasks = []store.Task{
		{ID: "t_1", Title: "Book the ferry", State: "open"},
		{ID: "t_2", Title: "Buy milk", State: "done"},
	}
	out, err := run(t, f, "done", "ferry")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || f.commands[0].Type != "task_complete" {
		t.Fatalf("want one task_complete, got %+v", f.commands)
	}
	if !strings.Contains(out, "done: Book the ferry") {
		t.Errorf("output: %q", out)
	}
}

func TestDoneRefusesSeveralMatches(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	f.tasks = []store.Task{
		{ID: "t_1", Title: "Book the ferry", State: "open"},
		{ID: "t_2", Title: "Book the hotel", State: "open"},
	}
	out, err := run(t, f, "done", "Book")
	if err == nil {
		t.Fatal("want an error for two matches")
	}
	if len(f.commands) != 0 {
		t.Errorf("nothing must change: %+v", f.commands)
	}
	for _, want := range []string{"t_1", "t_2", "Book the ferry", "Book the hotel"} {
		if !strings.Contains(out, want) {
			t.Errorf("output has no %q:\n%s", want, out)
		}
	}
}

func TestDoneTakesAnID(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	f.tasks = []store.Task{
		{ID: "t_1", Title: "Book the ferry", State: "open"},
		{ID: "t_2", Title: "Book the hotel", State: "open"},
	}
	if _, err := run(t, f, "done", "t_2"); err != nil {
		t.Fatal(err)
	}
	var args store.IDArgs
	_ = json.Unmarshal(f.commands[0].Args, &args)
	if args.ID != "t_2" {
		t.Errorf("id: got %q", args.ID)
	}
}

func TestDoneSaysNothingMatches(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	if _, err := run(t, f, "done", "ferry"); err == nil {
		t.Fatal("want an error when no task matches")
	}
}

func TestProjectsCountOpenTasks(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	f.projects = append(f.projects, store.Project{ID: "p1", Name: "Home"})
	f.tasks = []store.Task{
		{ID: "t_1", ProjectID: "p1", Title: "a", State: "open"},
		{ID: "t_2", ProjectID: "p1", Title: "b", State: "done"},
		{ID: "t_3", ProjectID: "p1", Title: "c", State: "open", DeletedAt: ptr("2026-01-01")},
		{ID: "t_4", ProjectID: "inbox", Title: "d", State: "open"},
	}
	out, err := run(t, f, "projects")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want a header and two projects, got:\n%s", out)
	}
	if !strings.Contains(lines[1], "Inbox") || !strings.HasSuffix(strings.TrimSpace(lines[1]), "1") {
		t.Errorf("inbox line: %q", lines[1])
	}
	if !strings.Contains(lines[2], "Home") || !strings.HasSuffix(strings.TrimSpace(lines[2]), "1") {
		t.Errorf("home line: %q", lines[2])
	}
}

func TestUnknownOptionAndCommand(t *testing.T) {
	t.Setenv("TEHA_TOKEN", "t")
	f := newFakeServer(t)
	if _, err := run(t, f, "ls", "--nope"); err == nil {
		t.Error("want an error for an unknown option")
	}
	if _, err := run(t, f, "wat"); err == nil {
		t.Error("want an error for an unknown command")
	}
}

func TestOptionsComeFromAnyPosition(t *testing.T) {
	pos, opt, err := parseOptions([]string{"Buy milk", "--server=http://x:1", "--limit", "7"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pos) != 1 || pos[0] != "Buy milk" {
		t.Errorf("positional: got %v", pos)
	}
	if opt.server != "http://x:1" || opt.limit != 7 {
		t.Errorf("options: got %+v", opt)
	}
}

func TestServerComesFromTheEnvironment(t *testing.T) {
	t.Setenv("TEHA_SERVER", "http://127.0.0.1:9999")
	_, opt, err := parseOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opt.server != "http://127.0.0.1:9999" {
		t.Errorf("server: got %q", opt.server)
	}
}

func TestTokenFileNeedsTightPermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("TEHA_TOKEN", "")
	path := filepath.Join(dir, "teha", "token")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("file-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadToken()
	if err == nil {
		t.Fatal("want a refusal for a world readable token file")
	}
	if strings.Contains(err.Error(), "file-token") {
		t.Error("the error carries the token")
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("the error must say how to fix it: %v", err)
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != "file-token" {
		t.Errorf("token: got %q", got)
	}
}

func TestEnvironmentTokenWinsOverTheFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("TEHA_TOKEN", "env-token")
	got, err := loadToken()
	if err != nil || got != "env-token" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestMissingTokenFileIsNotAnError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TEHA_TOKEN", "")
	got, err := loadToken()
	if err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestHelpFitsOneScreen(t *testing.T) {
	lines := strings.Split(strings.TrimRight(usage, "\n"), "\n")
	if len(lines) > 24 {
		t.Errorf("the help text has %d lines, which is more than one screen", len(lines))
	}
	for _, l := range lines {
		if len(l) > 79 {
			t.Errorf("this line is wider than 80 columns: %q", l)
		}
	}
}
