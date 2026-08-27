// SPDX-License-Identifier: AGPL-3.0-or-later

package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/lightheaded/teha/internal/store"
)

// The clock every test counts from. Nothing here reads the wall clock.
var base = time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// testKeys makes a VAPID keypair inside the test. No key of any kind, not even
// a throwaway one, belongs in the repository.
func testKeys(t *testing.T) Keys {
	t.Helper()
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	return Keys{Public: public, Private: private, Subject: "https://teha.example/owner"}
}

// device makes a subscription with a real P-256 public key, so the library can
// encrypt to it exactly as it would for a browser.
func device(t *testing.T, endpoint string) store.PushSubscription {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return store.PushSubscription{
		Endpoint:  endpoint,
		P256dh:    base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()),
		Auth:      base64.RawURLEncoding.EncodeToString(auth),
		UserAgent: "a test",
	}
}

func openStore(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Now = func() time.Time { return base }
	return st
}

func apply(t *testing.T, st *store.Store, uuid, kind string, args any) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	_, res, err := st.Apply([]store.Command{{UUID: uuid, Type: kind, Args: raw}})
	if err != nil {
		t.Fatal(err)
	}
	if !res[0].OK {
		t.Fatalf("%s failed: %s", kind, res[0].Error)
	}
}

func ptr[T any](v T) *T { return &v }

// oneDueReminder writes a task and a reminder that is due at base.
func oneDueReminder(t *testing.T, st *store.Store) {
	t.Helper()
	apply(t, st, "u1", "task_add", store.TaskArgs{
		ID: "t1", Title: ptr("Call the garage"), DueDate: ptr("2026-08-27"), DueTime: ptr("09:00"),
	})
	apply(t, st, "u2", "reminder_add", store.ReminderArgs{
		ID: "r1", TaskID: ptr("t1"), Kind: ptr(store.KindAtDue),
		FireAt: ptr(base.Add(-time.Minute).Format(time.RFC3339)),
	})
}

func newSender(t *testing.T, st *store.Store) *Sender {
	t.Helper()
	s := New(st, testKeys(t), quietLog())
	s.Now = func() time.Time { return base }
	s.SendTimeout = 2 * time.Second
	s.Client = &http.Client{Timeout: 2 * time.Second}
	return s
}

// pushService is an httptest server that stands in for a real push service.
func pushService(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestATickSendsOneNotificationPerDevice(t *testing.T) {
	var hits atomic.Int64
	srv := pushService(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") == "" {
			t.Error("a push must carry the VAPID Authorization header")
		}
		if got := r.Header.Get("Content-Encoding"); got != "aes128gcm" {
			t.Errorf("want aes128gcm, got %q", got)
		}
		w.WriteHeader(http.StatusCreated)
	})

	st := openStore(t, filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	oneDueReminder(t, st)
	if err := st.SaveSubscription(device(t, srv.URL+"/send/one")); err != nil {
		t.Fatal(err)
	}

	s := newSender(t, st)
	sent, err := s.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sent != 1 || hits.Load() != 1 {
		t.Fatalf("want one push, got sent=%d hits=%d", sent, hits.Load())
	}

	// A second pass has nothing to do.
	sent, err = s.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sent != 0 || hits.Load() != 1 {
		t.Fatalf("the reminder fired already, got sent=%d hits=%d", sent, hits.Load())
	}
}

// A 404 or a 410 means the browser threw its subscription away. Keeping the row
// would cost a round trip on every pass, for ever.
func TestADeadSubscriptionIsDeleted(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := pushService(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			})
			st := openStore(t, filepath.Join(t.TempDir(), "t.db"))
			defer st.Close()
			oneDueReminder(t, st)
			if err := st.SaveSubscription(device(t, srv.URL+"/send/dead")); err != nil {
				t.Fatal(err)
			}

			s := newSender(t, st)
			sent, err := s.Tick(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if sent != 0 {
				t.Fatalf("a dead device received nothing, got sent=%d", sent)
			}
			n, err := st.CountSubscriptions()
			if err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatalf("the subscription must be gone, got %d rows", n)
			}
		})
	}
}

// A 429 asks for a pause. The deadline goes on disk, so a restart honours it.
func TestATooManyRequestsAnswerBacksOff(t *testing.T) {
	srv := pushService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	st := openStore(t, filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	oneDueReminder(t, st)
	sub := device(t, srv.URL+"/send/busy")
	if err := st.SaveSubscription(sub); err != nil {
		t.Fatal(err)
	}

	s := newSender(t, st)
	if _, err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	until, err := st.RetryUntil(sub.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	want := base.Add(120 * time.Second).UTC().Format(time.RFC3339)
	if until != want {
		t.Fatalf("want a back-off until %s, got %q", want, until)
	}
	// The subscription is still there. A 429 is a pause, not a death.
	if n, _ := st.CountSubscriptions(); n != 1 {
		t.Fatalf("a paused device is not a dead device, got %d rows", n)
	}
	live, err := st.Subscriptions(base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 0 {
		t.Fatal("nothing goes to a paused device")
	}
	live, err = st.Subscriptions(base.Add(3 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 {
		t.Fatal("the pause must end")
	}
}

func TestRetryAfterReadsBothForms(t *testing.T) {
	cases := []struct {
		header string
		want   time.Duration
	}{
		{"30", 30 * time.Second},
		{"", time.Minute},
		{"nonsense", time.Minute},
		{"0", time.Minute},
		{base.Add(90 * time.Second).UTC().Format(http.TimeFormat), 90 * time.Second},
		{"999999", MaxBackOff},
	}
	for _, c := range cases {
		if got := parseRetryAfter(c.header, base, time.Minute); got != c.want {
			t.Errorf("Retry-After %q: want %s, got %s", c.header, c.want, got)
		}
	}
}

// One push service that accepts the connection and then says nothing must not
// hold the scheduler, and must not stop the other device from being reached.
func TestAHangingPushServiceDoesNotStallTheScheduler(t *testing.T) {
	done := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	// The order matters: close the channel first, so the handler returns and
	// the server can shut down.
	t.Cleanup(hang.Close)
	t.Cleanup(func() { close(done) })

	var fast atomic.Int64
	quick := pushService(t, func(w http.ResponseWriter, r *http.Request) {
		fast.Add(1)
		w.WriteHeader(http.StatusCreated)
	})

	st := openStore(t, filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	oneDueReminder(t, st)
	if err := st.SaveSubscription(device(t, hang.URL+"/send/slow")); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSubscription(device(t, quick.URL+"/send/fast")); err != nil {
		t.Fatal(err)
	}

	s := newSender(t, st)
	s.SendTimeout = 200 * time.Millisecond
	s.Client = &http.Client{Timeout: time.Second}

	start := time.Now()
	sent, err := s.Tick(context.Background())
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if took > 3*time.Second {
		t.Fatalf("the pass waited %s on a hanging push service", took)
	}
	if sent != 1 || fast.Load() != 1 {
		t.Fatalf("the working device must still get its push, got sent=%d fast=%d", sent, fast.Load())
	}
}

// The whole once-only guarantee, end to end: one push leaves the process, the
// process restarts, and no second push follows. See D-010.
func TestOnceOnlyAcrossARestart(t *testing.T) {
	var hits atomic.Int64
	srv := pushService(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusCreated)
	})

	path := filepath.Join(t.TempDir(), "restart.db")
	st := openStore(t, path)
	oneDueReminder(t, st)
	sub := device(t, srv.URL+"/send/one")
	if err := st.SaveSubscription(sub); err != nil {
		t.Fatal(err)
	}

	s := newSender(t, st)
	if _, err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("want one push before the restart, got %d", hits.Load())
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	again := openStore(t, path)
	defer again.Close()
	s2 := newSender(t, again)
	s2.Now = func() time.Time { return base.Add(time.Minute) }
	if _, err := s2.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("the restart sent the reminder again: %d pushes", hits.Load())
	}
}

// With nobody subscribed the pass claims nothing, so a reminder is not thrown
// away in silence before the first device arrives.
func TestNoSubscriptionMeansNoClaim(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	oneDueReminder(t, st)

	s := newSender(t, st)
	if sent, err := s.Tick(context.Background()); err != nil || sent != 0 {
		t.Fatalf("want nothing sent, got %d %v", sent, err)
	}

	claim, err := st.ClaimDue(base, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.Due) != 1 {
		t.Fatalf("the reminder must still be waiting, got %d", len(claim.Due))
	}
}

func TestTheMessageSaysSomethingUseful(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	apply(t, st, "u1", "task_add", store.TaskArgs{
		ID: "t1", Title: ptr("Call the garage"), DueDate: ptr("2026-08-27"), DueTime: ptr("09:00"),
	})
	apply(t, st, "u2", "task_add", store.TaskArgs{
		ID: "t2", Title: ptr("Book the ferry"), DueDate: ptr("2026-08-27"),
	})
	s := newSender(t, st)

	body, ok := s.payload(store.DueReminder{
		ID: "r1", Kind: store.KindAtDue, TaskID: "t1", Title: "Call the garage", DueTime: "09:00",
	}, base)
	if !ok {
		t.Fatal("an at-due reminder must produce a message")
	}
	var n notification
	if err := json.Unmarshal(body, &n); err != nil {
		t.Fatal(err)
	}
	if n.Title != "Call the garage" || n.Body != "Due at 09:00" {
		t.Fatalf("wrong wording: %+v", n)
	}
	if n.URL != "/?task=t1" {
		t.Fatalf("the notification must open the task, got %q", n.URL)
	}
	if n.Tag != "teha-r1" {
		t.Fatalf("the tag collapses a duplicate in the tray, got %q", n.Tag)
	}

	body, ok = s.payload(store.DueReminder{
		ID: "r2", Kind: store.KindBeforeDue, TaskID: "t1", Title: "Call the garage",
		DueTime: "09:00", OffsetMin: 30,
	}, base)
	if !ok {
		t.Fatal("a before-due reminder must produce a message")
	}
	if err := json.Unmarshal(body, &n); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.Body, "Due in 30 minutes") {
		t.Fatalf("wrong wording: %q", n.Body)
	}

	body, ok = s.payload(store.DueReminder{ID: "d1", Kind: store.KindDailyDigest}, base)
	if !ok {
		t.Fatal("a digest with two tasks due must produce a message")
	}
	if err := json.Unmarshal(body, &n); err != nil {
		t.Fatal(err)
	}
	if n.Title != "Today" || !strings.HasPrefix(n.Body, "2 tasks due") {
		t.Fatalf("wrong digest wording: %+v", n)
	}

	// A digest with nothing due says nothing at all.
	if _, ok := s.payload(store.DueReminder{ID: "d2", Kind: store.KindDailyDigest},
		base.Add(-72*time.Hour)); ok {
		t.Fatal("an empty digest must not be sent")
	}
}

func TestMinutesReadLikeAPersonSaysThem(t *testing.T) {
	cases := map[int]string{
		0: "a moment", 1: "1 minute", 30: "30 minutes", 60: "1 hour",
		120: "2 hours", 90: "1 hour", 1440: "1 day", 2880: "2 days",
	}
	for in, want := range cases {
		if got := minutes(in); got != want {
			t.Errorf("minutes(%d): want %q, got %q", in, want, got)
		}
	}
}

func TestSendTestReachesEveryDevice(t *testing.T) {
	var hits atomic.Int64
	srv := pushService(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusCreated)
	})
	st := openStore(t, filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	if err := st.SaveSubscription(device(t, srv.URL+"/a")); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSubscription(device(t, srv.URL+"/b")); err != nil {
		t.Fatal(err)
	}
	s := newSender(t, st)
	n, err := s.SendTest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || hits.Load() != 2 {
		t.Fatalf("want two pushes, got n=%d hits=%d", n, hits.Load())
	}
}
