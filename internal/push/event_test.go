// SPDX-License-Identifier: AGPL-3.0-or-later

package push

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lightheaded/teha/internal/store"
)

// The words of a notification are the whole product at the moment it arrives:
// a person reads two lines in a tray and decides whether to open the app.

func TestTheWordsOfAnEvent(t *testing.T) {
	s := newSender(t, openStore(t, filepath.Join(t.TempDir(), "push.db")))

	cases := []struct {
		name  string
		ev    store.Event
		title string
		body  string
	}{
		{
			name:  "a task given away",
			ev:    store.Event{Kind: store.EventAssigned, Actor: "Partner", TaskID: "t1", Title: "Buy milk"},
			title: "Buy milk",
			body:  "Partner gave this to you",
		},
		{
			name: "a comment",
			ev: store.Event{Kind: store.EventCommented, Actor: "Partner", TaskID: "t1",
				Title: "Buy milk", Body: "The green one, not the blue."},
			title: "Buy milk",
			body:  "Partner: The green one, not the blue.",
		},
		{
			name: "a comment with no name behind it",
			ev: store.Event{Kind: store.EventCommented, TaskID: "t1", Title: "Buy milk",
				Body: "Said by an account with no name"},
			title: "Buy milk",
			body:  "Somebody: Said by an account with no name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, ok := s.eventPayload(tc.ev)
			if !ok {
				t.Fatal("the event wrote no notification")
			}
			var n notification
			if err := json.Unmarshal(raw, &n); err != nil {
				t.Fatal(err)
			}
			if n.Title != tc.title || n.Body != tc.body {
				t.Errorf("got %q / %q, want %q / %q", n.Title, n.Body, tc.title, tc.body)
			}
			if n.URL != "/?task=t1" {
				t.Errorf("the notification must open the task, got %q", n.URL)
			}
			if !strings.Contains(n.Tag, tc.ev.Kind) || !strings.Contains(n.Tag, "t1") {
				t.Errorf("the tag must name the kind and the task, got %q", n.Tag)
			}
		})
	}

	// A long comment is cut to one line, and the whole text is one tap away.
	long := strings.Repeat("word ", 60)
	raw, ok := s.eventPayload(store.Event{Kind: store.EventCommented, Actor: "Partner",
		TaskID: "t1", Title: "Buy milk", Body: long})
	if !ok {
		t.Fatal("the event wrote no notification")
	}
	var n notification
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatal(err)
	}
	if len(n.Body) > 140 || !strings.HasSuffix(n.Body, "...") {
		t.Errorf("a long comment must be cut, got %d bytes: %q", len(n.Body), n.Body)
	}

	// A kind nobody wrote words for says nothing at all, rather than an empty
	// notification.
	if _, ok := s.eventPayload(store.Event{Kind: "invented", TaskID: "t1"}); ok {
		t.Error("an unknown kind must write no notification")
	}
}
