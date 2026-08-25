// SPDX-License-Identifier: AGPL-3.0-or-later

package todoist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// noSleep replaces the wait, so a backoff test runs in microseconds. The client
// still records every wait in Waits.
func noSleep(*Client) func(time.Duration) { return func(time.Duration) {} }

func TestClientBacksOffOn429(t *testing.T) {
	var got int
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got++
		auth = r.Header.Get("Authorization")
		if got <= 2 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"too many requests"}`))
			return
		}
		_, _ = w.Write([]byte(`{"sync_token":"abc","projects":[]}`))
	}))
	defer srv.Close()

	c := New("secret-token")
	c.Endpoint = srv.URL
	c.MinInterval = 0
	c.Sleep = noSleep(c)

	out, err := c.Sync(context.Background(), []string{"projects"})
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if out.SyncToken != "abc" {
		t.Errorf("sync token = %q, want abc", out.SyncToken)
	}
	if got != 3 {
		t.Errorf("requests = %d, want 3", got)
	}
	if auth != "Bearer secret-token" {
		t.Errorf("authorization header = %q", auth)
	}
	if len(c.Waits) != 2 {
		t.Fatalf("waits = %v, want two", c.Waits)
	}
	for _, w := range c.Waits {
		if w != 2*time.Second {
			t.Errorf("wait = %s, want the Retry-After value of 2s", w)
		}
	}
}

// TestClientBackoffGrows proves the exponential wait when the server sends no
// Retry-After header and never recovers.
func TestClientBackoffGrows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server broke", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New("secret-token")
	c.Endpoint = srv.URL
	c.MinInterval = 0
	c.MaxAttempts = 4
	c.Sleep = noSleep(c)

	if _, err := c.Sync(context.Background(), nil); err == nil {
		t.Fatal("Sync must fail after every attempt")
	}
	if c.Requests != 4 {
		t.Errorf("requests = %d, want 4", c.Requests)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if len(c.Waits) != len(want) {
		t.Fatalf("waits = %v, want %v", c.Waits, want)
	}
	for i, d := range want {
		if c.Waits[i] != d {
			t.Errorf("wait %d = %s, want %s", i, c.Waits[i], d)
		}
	}
}

// TestClientNoRetryOnBadToken checks two rules: a 401 does not improve with a
// retry, and the token never reaches an error message.
func TestClientNoRetryOnBadToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad token secret-token"}`))
	}))
	defer srv.Close()

	c := New("secret-token")
	c.Endpoint = srv.URL
	c.MinInterval = 0
	c.Sleep = noSleep(c)

	_, err := c.Sync(context.Background(), nil)
	if err == nil {
		t.Fatal("Sync must fail on a 401")
	}
	if c.Requests != 1 {
		t.Errorf("requests = %d, want 1", c.Requests)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Errorf("the error message holds the token: %s", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("the error message must mark the redaction: %s", err)
	}
}

// TestClientPacesAndPages checks the one request per second rule and the
// cursor loop of a paged read.
func TestClientPacesAndPages(t *testing.T) {
	var got int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got++
		if err := r.ParseForm(); err != nil {
			t.Errorf("the form did not parse: %v", err)
		}
		if got == 1 {
			if r.PostFormValue("sync_token") != "*" {
				t.Errorf("sync_token = %q, want *", r.PostFormValue("sync_token"))
			}
			_, _ = w.Write([]byte(`{"sync_token":"a","next_cursor":"page2","projects":[{"id":"1","name":"Home"}]}`))
			return
		}
		if r.PostFormValue("cursor") != "page2" {
			t.Errorf("cursor = %q, want page2", r.PostFormValue("cursor"))
		}
		_, _ = w.Write([]byte(`{"sync_token":"b","projects":[{"id":"2","name":"Work"}]}`))
	}))
	defer srv.Close()

	c := New("secret-token")
	c.Endpoint = srv.URL
	c.Sleep = noSleep(c)

	out, err := c.Sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
	if len(out.Projects) != 2 || out.Projects[1].Name != "Work" {
		t.Errorf("the pages did not merge: %+v", out.Projects)
	}
	if out.SyncToken != "b" {
		t.Errorf("sync token = %q, want b", out.SyncToken)
	}
	if len(c.Waits) != 1 || c.Waits[0] <= 0 || c.Waits[0] > time.Second {
		t.Errorf("waits = %v, want one wait under a second", c.Waits)
	}
}
