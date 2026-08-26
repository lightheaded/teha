// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lightheaded/teha/internal/store"
)

func newServer(t *testing.T, token string) (*Server, *httptest.Server) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return fixed }
	t.Cleanup(func() { st.Close() })

	s := New(st, token, slog.New(slog.DiscardHandler))
	s.Now = func() time.Time { return fixed }
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	return s, ts
}

func do(t *testing.T, ts *httptest.Server, method, path, token, body string) (int, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, r)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(out)
}

func TestTokenGuardsEveryRoute(t *testing.T) {
	_, ts := newServer(t, "secret")
	for _, path := range []string{"/v1/sync", "/v1/tasks", "/v1/projects", "/v1/export"} {
		method := http.MethodGet
		body := ""
		if path == "/v1/sync" {
			method, body = http.MethodPost, `{"since":0}`
		}
		if code, _ := do(t, ts, method, path, "", body); code != http.StatusUnauthorized {
			t.Errorf("%s without a token returned %d, want 401", path, code)
		}
		if code, _ := do(t, ts, method, path, "wrong", body); code != http.StatusUnauthorized {
			t.Errorf("%s with a wrong token returned %d, want 401", path, code)
		}
		if code, _ := do(t, ts, method, path, "secret", body); code != http.StatusOK {
			t.Errorf("%s with the right token returned %d, want 200", path, code)
		}
	}
}

func TestHealthNeedsNoToken(t *testing.T) {
	_, ts := newServer(t, "secret")
	code, body := do(t, ts, http.MethodGet, "/v1/health", "", "")
	if code != http.StatusOK || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("health returned %d %s", code, body)
	}
}

func TestSyncRoundTrip(t *testing.T) {
	_, ts := newServer(t, "")
	code, body := do(t, ts, http.MethodPost, "/v1/sync", "", `{"since":0,"commands":[
		{"uuid":"a1","type":"task_add","args":{"id":"t1","title":"Buy milk","due_date":"2026-08-25"}},
		{"uuid":"a2","type":"task_add","args":{"id":"t2","title":"No date task"}}]}`)
	if code != http.StatusOK {
		t.Fatalf("sync returned %d: %s", code, body)
	}
	var res struct {
		Version int64 `json:"version"`
		Applied []struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"applied"`
		Tasks []store.Task `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 2 || !res.Applied[0].OK || !res.Applied[1].OK {
		t.Fatalf("applied: %+v", res.Applied)
	}
	if len(res.Tasks) != 2 || res.Version == 0 {
		t.Fatalf("response carried %d tasks at version %d", len(res.Tasks), res.Version)
	}

	// A pull at the current version returns nothing new.
	code, body = do(t, ts, http.MethodPost, "/v1/sync", "", `{"since":`+itoa(res.Version)+`}`)
	if code != http.StatusOK {
		t.Fatal(body)
	}
	var second struct {
		Tasks []store.Task `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(body), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Tasks) != 0 {
		t.Fatalf("a pull at the head returned %d tasks", len(second.Tasks))
	}
}

func TestSyncRejectsHugeBatch(t *testing.T) {
	_, ts := newServer(t, "")
	var b strings.Builder
	b.WriteString(`{"since":0,"commands":[`)
	for i := 0; i < 201; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"uuid":"u` + itoa(int64(i)) + `","type":"task_add","args":{"id":"x` + itoa(int64(i)) + `","title":"x"}}`)
	}
	b.WriteString(`]}`)
	code, body := do(t, ts, http.MethodPost, "/v1/sync", "", b.String())
	if code != http.StatusBadRequest || !strings.Contains(body, "200 commands") {
		t.Fatalf("a batch of 201 returned %d: %s", code, body)
	}
}

func TestTasksFilter(t *testing.T) {
	_, ts := newServer(t, "")
	do(t, ts, http.MethodPost, "/v1/sync", "", `{"since":0,"commands":[
		{"uuid":"a1","type":"task_add","args":{"id":"t1","title":"Due today","due_date":"2026-08-25"}},
		{"uuid":"a2","type":"task_add","args":{"id":"t2","title":"Late","due_date":"2026-08-01"}},
		{"uuid":"a3","type":"task_add","args":{"id":"t3","title":"Someday"}}]}`)

	cases := []struct {
		filter string
		want   int
	}{
		{"today", 2}, // today includes what is overdue, like Todoist
		{"overdue", 1},
		{"no date", 1},
		{"", 3},
	}
	for _, c := range cases {
		code, body := do(t, ts, http.MethodGet, "/v1/tasks?filter="+url.QueryEscape(c.filter), "", "")
		if code != http.StatusOK {
			t.Fatalf("%q returned %d: %s", c.filter, code, body)
		}
		var res struct {
			Tasks []store.Task `json:"tasks"`
		}
		if err := json.Unmarshal([]byte(body), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.Tasks) != c.want {
			t.Errorf("filter %q returned %d tasks, want %d", c.filter, len(res.Tasks), c.want)
		}
	}
}

func TestBadFilterIsARequestError(t *testing.T) {
	_, ts := newServer(t, "")
	code, body := do(t, ts, http.MethodGet, "/v1/tasks?filter=today%20%26%20(", "", "")
	if code != http.StatusBadRequest {
		t.Fatalf("a broken filter returned %d: %s", code, body)
	}
	if !strings.Contains(body, "error") {
		t.Fatalf("the response carries no reason: %s", body)
	}
}

func TestExportHoldsEverything(t *testing.T) {
	_, ts := newServer(t, "")
	do(t, ts, http.MethodPost, "/v1/sync", "", `{"since":0,"commands":[
		{"uuid":"p1","type":"project_add","args":{"id":"p1","name":"Home"}},
		{"uuid":"a1","type":"task_add","args":{"id":"t1","title":"Fix the roof","project_id":"p1","labels":["home"]}}]}`)
	code, body := do(t, ts, http.MethodGet, "/v1/export", "", "")
	if code != http.StatusOK {
		t.Fatal(body)
	}
	for _, want := range []string{`"Home"`, `"Fix the roof"`, `"home"`, `"version"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the export lacks %s", want)
		}
	}
}

// A write must wake the event stream, so another device pulls at once instead
// of waiting for a poll.
func TestEventStreamAnnouncesAWrite(t *testing.T) {
	s, ts := newServer(t, "")
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if got := res.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type is %q", got)
	}

	buf := make([]byte, 64)
	if _, err := res.Body.Read(buf); err != nil { // the connected comment
		t.Fatal(err)
	}

	done := make(chan string, 1)
	go func() {
		b := make([]byte, 128)
		n, err := res.Body.Read(b)
		if err != nil {
			done <- "read error: " + err.Error()
			return
		}
		done <- string(b[:n])
	}()

	time.Sleep(50 * time.Millisecond)
	s.Notify(42)

	select {
	case got := <-done:
		if !strings.Contains(got, "event: version") || !strings.Contains(got, "42") {
			t.Fatalf("the stream sent %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the stream said nothing about the write")
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

// A list field must never be null, whatever the request contained.
//
// Go marshals a nil slice to `null`, and a typed client that declares the field
// as a list then fails to parse the WHOLE answer, not just that field. The
// Android app hit this on its first connection test, which sends since=0 and no
// commands:
//
//	Expected start of the array '[', but had 'n' instead at path: $.applied
//
// The empty-database case is the one that broke, so it is the one under test.
func TestSyncNeverReturnsNullLists(t *testing.T) {
	_, ts := newServer(t, "")
	defer ts.Close()

	cases := map[string]string{
		"no commands, empty database": `{"since":0,"commands":[]}`,
		"commands omitted":            `{"since":0}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			code, out := do(t, ts, "POST", "/v1/sync", "", body)
			if code != http.StatusOK {
				t.Fatalf("status %d: %s", code, out)
			}
			for _, field := range []string{"applied", "projects", "labels", "tasks"} {
				if strings.Contains(out, `"`+field+`":null`) {
					t.Errorf("%q is null in the answer, and must be an empty list: %s", field, out)
				}
			}
			// Decode into the shape a typed client uses. A nil slice would have
			// been caught above, so this guards the rest of the contract.
			var got struct {
				Version  int64           `json:"version"`
				Applied  []store.Result  `json:"applied"`
				Projects []store.Project `json:"projects"`
				Labels   []store.Label   `json:"labels"`
				Tasks    []store.Task    `json:"tasks"`
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("cannot decode: %v", err)
			}
			if got.Applied == nil || got.Projects == nil || got.Labels == nil || got.Tasks == nil {
				t.Errorf("a list decoded to nil: %s", out)
			}
		})
	}
}

// syncBody is the part of a sync answer that the reschedule test reads.
type syncBody struct {
	Applied []store.Result `json:"applied"`
	Tasks   []struct {
		ID        string `json:"id"`
		DueDate   string `json:"due_date"`
		DueTime   string `json:"due_time"`
		RRule     string `json:"rrule"`
		ProjectID string `json:"project_id"`
		Priority  int    `json:"priority"`
		State     string `json:"state"`
		DeletedAt string `json:"deleted_at"`
	} `json:"tasks"`
}

func decodeSync(t *testing.T, body string) syncBody {
	t.Helper()
	var out syncBody
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestBulkRescheduleInOneRequest locks the contract that both clients now
// depend on for their "Reschedule" button.
//
// A bulk reschedule is one task_update per task in one request. It is not one
// command that says "everything overdue": a command names an id and a date, so
// a replay from an outbox does the same thing tomorrow, while a command that
// carried a query would mean something different every time the server ran it.
//
// The test also proves the two rules the clients rely on:
//   - a command that does not name due_time leaves the time alone
//   - clearing a date can clear the time with it, so no row is left holding a
//     time and no day, which is a row that no view can print
func TestBulkRescheduleInOneRequest(t *testing.T) {
	_, ts := newServer(t, "tok")

	add := `{"since":0,"commands":[
		{"uuid":"a1","type":"task_add","args":{"id":"t1","title":"One","due_date":"2026-08-20"}},
		{"uuid":"a2","type":"task_add","args":{"id":"t2","title":"Two","due_date":"2026-08-21","due_time":"09:00"}},
		{"uuid":"a3","type":"task_add","args":{"id":"t3","title":"Three","due_date":"2026-08-22","rrule":"FREQ=WEEKLY"}}]}`
	if code, body := do(t, ts, "POST", "/v1/sync", "tok", add); code != http.StatusOK {
		t.Fatalf("seed failed: %d %s", code, body)
	}

	move := `{"since":0,"commands":[
		{"uuid":"b1","type":"task_update","args":{"id":"t1","due_date":"2026-08-25"}},
		{"uuid":"b2","type":"task_update","args":{"id":"t2","due_date":"2026-08-25"}},
		{"uuid":"b3","type":"task_update","args":{"id":"t3","due_date":"2026-08-25"}}]}`
	code, body := do(t, ts, "POST", "/v1/sync", "tok", move)
	if code != http.StatusOK {
		t.Fatalf("reschedule failed: %d %s", code, body)
	}
	got := decodeSync(t, body)
	if len(got.Applied) != 3 {
		t.Fatalf("applied %d commands, want 3: %s", len(got.Applied), body)
	}
	for _, r := range got.Applied {
		if !r.OK {
			t.Fatalf("command %s failed: %s", r.UUID, r.Error)
		}
	}
	for _, task := range got.Tasks {
		if task.DueDate != "2026-08-25" {
			t.Errorf("task %s is due %q, want 2026-08-25", task.ID, task.DueDate)
		}
	}
	for _, task := range got.Tasks {
		switch task.ID {
		case "t2":
			// The command never named the time, so the time stays.
			if task.DueTime != "09:00" {
				t.Errorf("task t2 lost its time: %q", task.DueTime)
			}
		case "t3":
			// A repeating task keeps repeating. Only the next date moved.
			if task.RRule != "FREQ=WEEKLY" {
				t.Errorf("task t3 lost its rule: %q", task.RRule)
			}
		}
	}

	clear := `{"since":0,"commands":[
		{"uuid":"c1","type":"task_update","args":{"id":"t2","clear":["due_date","due_time"]}}]}`
	code, body = do(t, ts, "POST", "/v1/sync", "tok", clear)
	if code != http.StatusOK {
		t.Fatalf("clear failed: %d %s", code, body)
	}
	// A fresh value, not the one above. encoding/json reuses the elements of a
	// slice it decodes into, and an absent field then keeps the old value, so a
	// second decode into the same variable reads a stale date.
	after := decodeSync(t, body)
	for _, task := range after.Tasks {
		if task.ID != "t2" {
			continue
		}
		if task.DueDate != "" || task.DueTime != "" {
			t.Fatalf("task t2 kept a due field: date %q time %q", task.DueDate, task.DueTime)
		}
	}
}

// TestMixedBulkActionsInOneRequest locks the wire shape for every bulk action
// the clients offer, not only the reschedule.
//
// D-008 says a bulk action is one command per task. This test sends four
// different kinds of bulk action in one request, because that is what a person
// who marks a set of tasks and works through it produces: a move, a priority,
// a completion and a delete, each naming its own task.
//
// One request, one transaction. A batch that half applied would leave the
// screen and the account disagreeing, with no way for the client to tell.
func TestMixedBulkActionsInOneRequest(t *testing.T) {
	_, ts := newServer(t, "tok")

	seed := `{"since":0,"commands":[
		{"uuid":"p1","type":"project_add","args":{"id":"pr1","name":"Shopping"}},
		{"uuid":"a1","type":"task_add","args":{"id":"t1","title":"One"}},
		{"uuid":"a2","type":"task_add","args":{"id":"t2","title":"Two"}},
		{"uuid":"a3","type":"task_add","args":{"id":"t3","title":"Three"}},
		{"uuid":"a4","type":"task_add","args":{"id":"t4","title":"Four"}}]}`
	if code, body := do(t, ts, "POST", "/v1/sync", "tok", seed); code != http.StatusOK {
		t.Fatalf("seed failed: %d %s", code, body)
	}

	bulk := `{"since":0,"commands":[
		{"uuid":"b1","type":"task_update","args":{"id":"t1","project_id":"pr1"}},
		{"uuid":"b2","type":"task_update","args":{"id":"t2","project_id":"pr1"}},
		{"uuid":"b3","type":"task_update","args":{"id":"t3","priority":1}},
		{"uuid":"b4","type":"task_complete","args":{"id":"t4"}},
		{"uuid":"b5","type":"task_delete","args":{"id":"t3"}}]}`
	code, body := do(t, ts, "POST", "/v1/sync", "tok", bulk)
	if code != http.StatusOK {
		t.Fatalf("bulk failed: %d %s", code, body)
	}
	got := decodeSync(t, body)
	if len(got.Applied) != 5 {
		t.Fatalf("applied %d commands, want 5: %s", len(got.Applied), body)
	}
	for _, r := range got.Applied {
		if !r.OK {
			t.Fatalf("command %s failed: %s", r.UUID, r.Error)
		}
	}

	byID := map[string]struct {
		project  string
		priority int
		state    string
		deleted  bool
	}{}
	for _, task := range got.Tasks {
		byID[task.ID] = struct {
			project  string
			priority int
			state    string
			deleted  bool
		}{task.ProjectID, task.Priority, task.State, task.DeletedAt != ""}
	}

	if byID["t1"].project != "pr1" || byID["t2"].project != "pr1" {
		t.Errorf("the move did not apply: t1 in %q, t2 in %q", byID["t1"].project, byID["t2"].project)
	}
	if byID["t3"].priority != 1 {
		t.Errorf("t3 has priority %d, want 1", byID["t3"].priority)
	}
	// A delete after an update on the same task in the same batch. The order in
	// the request is the order the server applies, so the priority above is
	// kept and the row is also hidden.
	if !byID["t3"].deleted {
		t.Error("t3 is not deleted")
	}
	if byID["t4"].state != "done" {
		t.Errorf("t4 is %q, want done", byID["t4"].state)
	}
	// A task nobody named must not change. A bulk action names every task it
	// touches, so a row outside the set is proof that nothing leaked.
	if byID["t1"].state != "open" || byID["t1"].priority != 4 {
		t.Errorf("t1 changed beyond its move: state %q priority %d",
			byID["t1"].state, byID["t1"].priority)
	}

	// The undo path: a restore brings the row back, and one command per task
	// again.
	undo := `{"since":0,"commands":[
		{"uuid":"c1","type":"task_restore","args":{"id":"t3"}},
		{"uuid":"c2","type":"task_uncomplete","args":{"id":"t4"}}]}`
	code, body = do(t, ts, "POST", "/v1/sync", "tok", undo)
	if code != http.StatusOK {
		t.Fatalf("undo failed: %d %s", code, body)
	}
	after := decodeSync(t, body)
	for _, task := range after.Tasks {
		switch task.ID {
		case "t3":
			if task.DeletedAt != "" {
				t.Errorf("t3 is still deleted: %q", task.DeletedAt)
			}
			if task.Priority != 1 {
				t.Errorf("t3 lost its priority through the delete: %d", task.Priority)
			}
		case "t4":
			if task.State != "open" {
				t.Errorf("t4 is %q, want open", task.State)
			}
		}
	}
}
