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
