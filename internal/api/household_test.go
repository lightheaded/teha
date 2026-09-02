// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lightheaded/teha/internal/store"
)

// The household over HTTP, from the outside: the owner writes an invitation,
// somebody joins with it, the two accounts stay apart, and a shared list
// reaches both. This is the M6 exit test in miniature.

const ownerToken = "owner-token"

// join redeems a code and returns the new account's device token.
func join(t *testing.T, ts *httptest.Server, code, name string) string {
	t.Helper()
	code = strings.TrimSpace(code)
	body := `{"code":"` + code + `","name":"` + name + `"}`
	status, out := do(t, ts, http.MethodPost, "/v1/join", "", body)
	if status != http.StatusOK {
		t.Fatalf("join returned %d: %s", status, out)
	}
	var d struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if d.Token == "" {
		t.Fatal("joining must answer with a device token")
	}
	return d.Token
}

func TestAPartnerJoinsAndTheTwoAccountsStayApart(t *testing.T) {
	_, ts := newServer(t, ownerToken)

	// The owner writes an invitation.
	status, out := do(t, ts, http.MethodPost, "/v1/invites", ownerToken, `{"name":"partner"}`)
	if status != http.StatusOK {
		t.Fatalf("the invitation returned %d: %s", status, out)
	}
	var inv struct {
		ID   string `json:"id"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatal(err)
	}
	if inv.Code == "" {
		t.Fatal("an invitation must carry its code once")
	}

	// A second read of the list never shows the code again.
	_, list := do(t, ts, http.MethodGet, "/v1/invites", ownerToken, "")
	if strings.Contains(list, inv.Code) {
		t.Fatal("the code must be shown once and never again")
	}

	partnerToken := join(t, ts, inv.Code, "Partner")

	// Each one adds a task with no project. Each lands in its own inbox.
	mustSync(t, ts, ownerToken, `{"since":0,"commands":[
		{"uuid":"o1","type":"task_add","args":{"id":"t_owner","title":"The owner's task"}}]}`)
	mustSync(t, ts, partnerToken, `{"since":0,"commands":[
		{"uuid":"p1","type":"task_add","args":{"id":"t_partner","title":"The partner's task"}}]}`)

	ownerPull := mustSync(t, ts, ownerToken, `{"since":0,"commands":[]}`)
	partnerPull := mustSync(t, ts, partnerToken, `{"since":0,"commands":[]}`)

	if !hasTask(ownerPull, "t_owner") || hasTask(ownerPull, "t_partner") {
		t.Fatalf("the owner sees the wrong set: %s", ownerPull)
	}
	if !hasTask(partnerPull, "t_partner") || hasTask(partnerPull, "t_owner") {
		t.Fatalf("the partner sees the wrong set: %s", partnerPull)
	}

	// The partner cannot write into the owner's list.
	res := mustSync(t, ts, partnerToken, `{"since":0,"commands":[
		{"uuid":"p2","type":"task_update","args":{"id":"t_owner","title":"Mine now"}}]}`)
	if !strings.Contains(res, "cannot reach") {
		t.Fatalf("the write must be refused with one sentence: %s", res)
	}

	// Only the owner may invite.
	status, _ = do(t, ts, http.MethodPost, "/v1/invites", partnerToken, `{"name":"a friend"}`)
	if status != http.StatusForbidden {
		t.Fatalf("a member wrote an invitation, and only the owner may: %d", status)
	}
}

func TestASharedListReachesBothAndUnsharingAsksForAFreshPull(t *testing.T) {
	_, ts := newServer(t, ownerToken)

	_, out := do(t, ts, http.MethodPost, "/v1/invites", ownerToken, `{"name":"partner"}`)
	var inv struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatal(err)
	}
	partnerToken := join(t, ts, inv.Code, "Partner")

	// Who is in the house? The partner is allowed to ask.
	_, hh := do(t, ts, http.MethodGet, "/v1/household", partnerToken, "")
	if !strings.Contains(hh, "Partner") {
		t.Fatalf("the household must name the people: %s", hh)
	}

	// The owner makes a list and shares it.
	mustSync(t, ts, ownerToken, `{"since":0,"commands":[
		{"uuid":"o1","type":"project_add","args":{"id":"p_shop","name":"Shopping"}},
		{"uuid":"o2","type":"task_add","args":{"id":"t_milk","title":"Milk","project_id":"p_shop"}}]}`)

	var people struct {
		People []struct {
			ID   string `json:"id"`
			IsMe bool   `json:"is_me"`
		} `json:"people"`
	}
	_, hh = do(t, ts, http.MethodGet, "/v1/household", ownerToken, "")
	if err := json.Unmarshal([]byte(hh), &people); err != nil {
		t.Fatal(err)
	}
	partnerID := ""
	for _, p := range people.People {
		if !p.IsMe {
			partnerID = p.ID
		}
	}
	if partnerID == "" {
		t.Fatal("the household must name the other person")
	}

	status, out := do(t, ts, http.MethodPost, "/v1/share", ownerToken,
		`{"project_id":"p_shop","account_id":"`+partnerID+`","share":true}`)
	if status != http.StatusOK {
		t.Fatalf("sharing returned %d: %s", status, out)
	}

	got := mustSync(t, ts, partnerToken, `{"since":0,"commands":[]}`)
	if !hasTask(got, "t_milk") {
		t.Fatalf("a shared list must reach the other person: %s", got)
	}

	// The partner ticks it off, and the owner sees that.
	mustSync(t, ts, partnerToken, `{"since":0,"commands":[
		{"uuid":"p9","type":"task_complete","args":{"id":"t_milk"}}]}`)
	owner := mustSync(t, ts, ownerToken, `{"since":0,"commands":[]}`)
	if !strings.Contains(owner, `"state":"done"`) {
		t.Fatalf("the tick must reach the owner: %s", owner)
	}

	// The partner cannot pass the list on, and cannot rename it.
	status, _ = do(t, ts, http.MethodPost, "/v1/share", partnerToken,
		`{"project_id":"p_shop","account_id":"owner","share":true}`)
	if status != http.StatusForbidden {
		t.Fatalf("a member shared a list that is not theirs: %d", status)
	}

	// The version the partner is up to date with.
	var at struct {
		Version int64 `json:"version"`
	}
	if err := json.Unmarshal([]byte(got), &at); err != nil {
		t.Fatal(err)
	}

	// The owner takes the list back. The next pull asks for a fresh start.
	if status, out := do(t, ts, http.MethodPost, "/v1/share", ownerToken,
		`{"project_id":"p_shop","account_id":"`+partnerID+`","share":false}`); status != http.StatusOK {
		t.Fatalf("unsharing returned %d: %s", status, out)
	}
	after := mustSync(t, ts, partnerToken, `{"since":`+itoa(at.Version)+`,"commands":[]}`)
	if !strings.Contains(after, `"reset":true`) {
		t.Fatalf("the pull must ask the client to start again: %s", after)
	}
	if hasTask(after, "t_milk") {
		t.Fatalf("the task is out of reach and must not be in the fresh pull: %s", after)
	}
}

// A person hears when somebody else gives them a task or says something on a
// task they share. Nobody hears their own action.
func TestAnAssignmentAndACommentReachTheOtherPerson(t *testing.T) {
	srv, ts := newServer(t, ownerToken)
	pusher := &fakePusher{key: "k"}
	srv.Push = pusher

	_, out := do(t, ts, http.MethodPost, "/v1/invites", ownerToken, `{"name":"partner"}`)
	var inv struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatal(err)
	}
	partnerToken := join(t, ts, inv.Code, "Partner")

	var people struct {
		People []struct {
			ID   string `json:"id"`
			IsMe bool   `json:"is_me"`
		} `json:"people"`
	}
	_, hh := do(t, ts, http.MethodGet, "/v1/household", ownerToken, "")
	if err := json.Unmarshal([]byte(hh), &people); err != nil {
		t.Fatal(err)
	}
	partnerID := ""
	for _, p := range people.People {
		if !p.IsMe {
			partnerID = p.ID
		}
	}

	mustSync(t, ts, ownerToken, `{"since":0,"commands":[
		{"uuid":"o1","type":"project_add","args":{"id":"p_shop","name":"Shopping"}},
		{"uuid":"o2","type":"task_add","args":{"id":"t_milk","title":"Milk","project_id":"p_shop"}}]}`)
	if status, out := do(t, ts, http.MethodPost, "/v1/share", ownerToken,
		`{"project_id":"p_shop","account_id":"`+partnerID+`","share":true}`); status != http.StatusOK {
		t.Fatalf("sharing returned %d: %s", status, out)
	}

	// The owner gives the task away, and then says something about it.
	mustSync(t, ts, ownerToken, `{"since":0,"commands":[
		{"uuid":"o3","type":"task_update","args":{"id":"t_milk","assignee_id":"`+partnerID+`"}},
		{"uuid":"o4","type":"comment_add","args":{"id":"cm_1","task_id":"t_milk","body":"The green one."}}]}`)

	got := pusher.took(t, 2)
	if got[0].Kind != store.EventAssigned || got[0].AccountID != partnerID {
		t.Fatalf("the assignment must reach the other person: %+v", got[0])
	}
	if got[1].Kind != store.EventCommented || got[1].AccountID != partnerID {
		t.Fatalf("the comment must reach the other person: %+v", got[1])
	}
	if got[1].Body != "The green one." || got[1].Title != "Milk" {
		t.Fatalf("the event must carry the words and the task: %+v", got[1])
	}
	if got[0].Actor == "" {
		t.Fatalf("the event must name who did it: %+v", got[0])
	}

	// The same command again changes nothing, so it must say nothing. And
	// giving a task to yourself is silent.
	mustSync(t, ts, partnerToken, `{"since":0,"commands":[
		{"uuid":"p1","type":"task_update","args":{"id":"t_milk","assignee_id":"`+partnerID+`"}}]}`)
	if extra := len(pusher.events); extra != 2 {
		t.Fatalf("a repeat or a self-assignment pushed again: %d events", extra)
	}
}

// --- helpers ----------------------------------------------------------------

func mustSync(t *testing.T, ts *httptest.Server, token, body string) string {
	t.Helper()
	status, out := do(t, ts, http.MethodPost, "/v1/sync", token, body)
	if status != http.StatusOK {
		t.Fatalf("sync returned %d: %s", status, out)
	}
	return out
}

func hasTask(payload, id string) bool {
	return strings.Contains(payload, `"id":"`+id+`"`)
}
