// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lightheaded/teha/internal/store"
)

func TestTheActivityRouteAnswersOnePageAtATime(t *testing.T) {
	_, ts := newServer(t, "tok")

	// Twelve writes, so the page and the cursor both have work to do.
	var cmds []string
	for i := 0; i < 12; i++ {
		cmds = append(cmds, `{"uuid":"u`+string(rune('a'+i))+`","type":"task_add",`+
			`"args":{"id":"t`+string(rune('a'+i))+`","title":"One"}}`)
	}
	code, body := do(t, ts, "POST", "/v1/sync", "tok",
		`{"since":0,"commands":[`+strings.Join(cmds, ",")+`]}`)
	if code != 200 {
		t.Fatalf("sync answered %d: %s", code, body)
	}

	code, body = do(t, ts, "GET", "/v1/activity?limit=5", "tok", "")
	if code != 200 {
		t.Fatalf("the activity route answered %d: %s", code, body)
	}
	var first struct {
		Activity []store.Activity `json:"activity"`
		More     bool             `json:"more"`
	}
	if err := json.Unmarshal([]byte(body), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Activity) != 5 {
		t.Fatalf("a page of five holds %d", len(first.Activity))
	}
	if !first.More {
		t.Fatal("twelve lines and a page of five must say that more exist")
	}

	// The cursor is the smallest seq of the page just read.
	last := first.Activity[len(first.Activity)-1].Seq
	code, body = do(t, ts, "GET", "/v1/activity?limit=50&before="+itoa(last), "tok", "")
	if code != 200 {
		t.Fatalf("the second page answered %d: %s", code, body)
	}
	var next struct {
		Activity []store.Activity `json:"activity"`
		More     bool             `json:"more"`
	}
	if err := json.Unmarshal([]byte(body), &next); err != nil {
		t.Fatal(err)
	}
	if len(next.Activity) != 7 {
		t.Fatalf("the rest holds %d lines, want 7", len(next.Activity))
	}
	if next.More {
		t.Fatal("the last page must not offer another one")
	}
}

func TestTheActivityRouteNeedsTheToken(t *testing.T) {
	_, ts := newServer(t, "tok")
	if code, _ := do(t, ts, "GET", "/v1/activity", "", ""); code != 401 {
		t.Fatalf("an unguarded activity route answered %d, want 401", code)
	}
}

func TestALoginIsInTheLog(t *testing.T) {
	s, ts := newServer(t, "tok")
	// The audit half of §6.6. A refused login names no account, so the owner
	// is who reads it.
	s.Store.Note(store.OwnerID, store.ActionLoginFailed, "", "the assertion did not verify", "192.0.2.9")

	code, body := do(t, ts, "GET", "/v1/activity", "tok", "")
	if code != 200 {
		t.Fatalf("the activity route answered %d: %s", code, body)
	}
	if !strings.Contains(body, store.ActionLoginFailed) {
		t.Fatalf("a failed login must be in the log: %s", body)
	}
	if !strings.Contains(body, "192.0.2.9") {
		t.Fatalf("a failed login must say where it came from: %s", body)
	}
}

