// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/lightheaded/teha/internal/store"
)

// fakePusher stands in for internal/push. The api package needs three methods,
// so a test needs three methods.
type fakePusher struct {
	mu   sync.Mutex
	key  string
	sent int
	// to records the account of the last test push, so a test can prove that
	// a notification goes to the person who asked for it.
	to string
	// events records what Deliver handed over. It is read from another
	// goroutine, so the mutex above guards it.
	events []store.Event
}

func (f *fakePusher) PublicKey() string { return f.key }
func (f *fakePusher) SendTestTo(_ context.Context, accountID string) (int, error) {
	f.sent++
	f.to = accountID
	return 1, nil
}

func (f *fakePusher) SendEvents(_ context.Context, events []store.Event) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, events...)
	return len(events), nil
}

// took waits for Deliver to hand the events over. Deliver runs in its own
// goroutine, so a test that read the field at once would race with it.
func (f *fakePusher) took(t *testing.T, want int) []store.Event {
	t.Helper()
	for i := 0; i < 200; i++ {
		f.mu.Lock()
		got := append([]store.Event{}, f.events...)
		f.mu.Unlock()
		if len(got) >= want {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the sender was handed %d events, want %d", len(f.events), want)
	return nil
}

func TestPushKeySaysWhetherPushIsOn(t *testing.T) {
	s, ts := newServer(t, "")

	code, body := do(t, ts, http.MethodGet, "/v1/push/key", "", "")
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", code, body)
	}
	var out struct {
		Enabled bool   `json:"enabled"`
		Key     string `json:"key"`
		Devices int    `json:"devices"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Enabled || out.Key != "" {
		t.Fatalf("a server with no key must say push is off, got %+v", out)
	}

	s.Push = &fakePusher{key: "a-public-key"}
	_, body = do(t, ts, http.MethodGet, "/v1/push/key", "", "")
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Enabled || out.Key != "a-public-key" {
		t.Fatalf("want the public key, got %+v", out)
	}
}

func TestSubscribeAndUnsubscribe(t *testing.T) {
	s, ts := newServer(t, "")
	sub := `{"endpoint":"https://push.example/abc","keys":{"p256dh":"a-key","auth":"a-secret"}}`

	if code, body := do(t, ts, http.MethodPost, "/v1/push/subscribe", "", sub); code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", code, body)
	}
	n, err := s.Store.CountSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want one subscription, got %d", n)
	}

	// The same browser subscribing again replaces its own row.
	if code, _ := do(t, ts, http.MethodPost, "/v1/push/subscribe", "", sub); code != http.StatusOK {
		t.Fatal("a re-subscribe must succeed")
	}
	if n, _ := s.Store.CountSubscriptions(); n != 1 {
		t.Fatalf("want one row, got %d", n)
	}

	if code, body := do(t, ts, http.MethodPost, "/v1/push/unsubscribe", "",
		`{"endpoint":"https://push.example/abc"}`); code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", code, body)
	}
	if n, _ := s.Store.CountSubscriptions(); n != 0 {
		t.Fatalf("want no rows, got %d", n)
	}
}

func TestSubscribeRefusesAnIncompleteSubscription(t *testing.T) {
	_, ts := newServer(t, "")
	code, _ := do(t, ts, http.MethodPost, "/v1/push/subscribe", "",
		`{"endpoint":"https://push.example/abc"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("want 400 for a subscription with no keys, got %d", code)
	}
}

func TestPushTestSaysSoWhenPushIsOff(t *testing.T) {
	s, ts := newServer(t, "")
	if code, _ := do(t, ts, http.MethodPost, "/v1/push/test", "", ""); code != http.StatusServiceUnavailable {
		t.Fatal("a server with no key must refuse the test and say why")
	}
	f := &fakePusher{key: "a-public-key"}
	s.Push = f
	if code, body := do(t, ts, http.MethodPost, "/v1/push/test", "", ""); code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", code, body)
	}
	if f.sent != 1 {
		t.Fatalf("want one test send, got %d", f.sent)
	}
}

func TestThePushRoutesNeedTheToken(t *testing.T) {
	_, ts := newServer(t, "secret")
	for _, path := range []string{"/v1/push/key", "/v1/push/subscribe", "/v1/push/unsubscribe", "/v1/push/test"} {
		method := http.MethodPost
		if path == "/v1/push/key" {
			method = http.MethodGet
		}
		if code, _ := do(t, ts, method, path, "", "{}"); code != http.StatusUnauthorized {
			t.Errorf("%s without a token returned %d, want 401", path, code)
		}
	}
}

func TestShortUANamesTheDevice(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128 Mobile Safari/537.36": "Chrome on Android",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/17.0 Safari/605.1.15":      "Safari on macOS",
		"Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0":                                 "Firefox on Linux",
	}
	for ua, want := range cases {
		if got := shortUA(ua); got != want {
			t.Errorf("shortUA(%.30s…): want %q, got %q", ua, want, got)
		}
	}
}
