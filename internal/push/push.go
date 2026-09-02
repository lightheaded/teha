// SPDX-License-Identifier: AGPL-3.0-or-later

// Package push sends Web Push notifications for due reminders.
//
// The transport is the Web Push protocol with VAPID, decided in
// docs/DECISIONS.md D-003: one server implementation reaches every desktop
// browser, Chrome on Android and an installed web app on iOS.
//
// The scheduler wakes on an interval, claims every due reminder in one
// transaction, and only then sends. The claim is the once-only guarantee and it
// lives in the store, next to the SQL that enforces it. See
// store.ClaimDue and docs/DECISIONS.md D-010 and D-011.
package push

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/lightheaded/teha/internal/store"
)

// Keys holds the VAPID identity of this server.
//
// The private key is a secret. It never enters the repository and it never
// enters a log line. It arrives from the environment, out of the encrypted
// store described in docs/DEV-SECRETS.md.
type Keys struct {
	Public  string
	Private string
	// Subject identifies the sender to the push service, as a mailto: address
	// or an https: URL. RFC 8292 asks for it, and a push service can use it to
	// reach the operator about a misbehaving sender.
	Subject string
}

// Defaults for the scheduler. Each one is a field, so a test sets its own.
const (
	DefaultInterval    = 30 * time.Second
	DefaultSendTimeout = 10 * time.Second
	DefaultTickTimeout = 2 * time.Minute
	// MaxBackOff caps what a Retry-After header can ask for. A push service
	// that asks for a week would otherwise silence the account for a week.
	MaxBackOff = 6 * time.Hour
)

// Sender is the scheduler and the sender in one. One instance runs per server.
type Sender struct {
	Store *store.Store
	Log   *slog.Logger
	Keys  Keys

	// Now returns the current time. Tests replace it, so no logic here reads
	// the wall clock. The same rule as api.Server.Now.
	Now func() time.Time

	Interval    time.Duration // how often the scheduler wakes
	SendTimeout time.Duration // deadline for one push request
	TickTimeout time.Duration // deadline for one whole pass
	Parallel    int           // how many pushes are in flight at once
	TTL         int           // seconds a push service holds an undelivered message

	// Notify wakes the event stream after the sent markers move, so a client
	// learns that a reminder fired. It can be nil.
	Notify func(int64)

	// Client sends the HTTP requests. Its timeout is a second guard under the
	// per-request deadline, because a push service that accepts a connection
	// and then says nothing must not hold a worker.
	Client *http.Client
}

// New builds a sender with the working defaults.
func New(st *store.Store, keys Keys, log *slog.Logger) *Sender {
	return &Sender{
		Store:       st,
		Log:         log,
		Keys:        keys,
		Now:         time.Now,
		Interval:    DefaultInterval,
		SendTimeout: DefaultSendTimeout,
		TickTimeout: DefaultTickTimeout,
		Parallel:    4,
		TTL:         int((6 * time.Hour).Seconds()),
		Client:      &http.Client{Timeout: DefaultSendTimeout},
	}
}

// PublicKey returns the VAPID public key. The browser needs it to subscribe.
func (s *Sender) PublicKey() string { return s.Keys.Public }

// Run wakes on the interval until the context ends.
//
// Each pass carries its own deadline, and each push inside it carries a
// shorter one. A push service that accepts the connection and then hangs
// therefore costs one deadline and never the scheduler. A ticker drops a tick
// that nobody read, so a slow pass cannot build a queue of passes either.
func (s *Sender) Run(ctx context.Context) {
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	s.Log.Info("the reminder scheduler is on", "interval", s.Interval.String())
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		tickCtx, cancel := context.WithTimeout(ctx, s.TickTimeout)
		if _, err := s.Tick(tickCtx); err != nil && ctx.Err() == nil {
			s.Log.Error("a reminder pass failed", "err", err)
		}
		cancel()
	}
}

// Tick runs one pass: claim, then send. It returns how many pushes the push
// services accepted.
func (s *Sender) Tick(ctx context.Context) (int, error) {
	now := s.Now()
	subs, err := s.Store.Subscriptions(now)
	if err != nil {
		return 0, err
	}
	// No device can receive anything, so claim nothing. A claim marks a
	// reminder sent, and marking one that nobody could receive would throw it
	// away in silence. When the first device subscribes, the grace window in
	// store.GraceWindow decides which of the waiting reminders still matter.
	if len(subs) == 0 {
		return 0, nil
	}

	claim, err := s.Store.ClaimDue(now, 200)
	if err != nil {
		return 0, err
	}
	if claim.Skipped > 0 {
		s.Log.Info("reminders came due while the server was down and are past their window",
			"count", claim.Skipped)
	}
	if len(claim.Due) == 0 {
		if claim.Skipped > 0 && s.Notify != nil {
			s.Notify(claim.Version)
		}
		return 0, nil
	}

	// A reminder goes to the devices of the person who set it, and to nobody
	// else's. The subscriptions are read once per account and kept, because a
	// batch usually carries several reminders of the same person.
	byAccount := map[string][]store.PushSubscription{}
	var sent int
	for _, d := range claim.Due {
		body, ok := s.payload(d, now)
		if !ok {
			continue // a digest with nothing in it says nothing
		}
		mine, seen := byAccount[d.AccountID]
		if !seen {
			mine, err = s.Store.SubscriptionsFor(now, d.AccountID)
			if err != nil {
				return sent, err
			}
			byAccount[d.AccountID] = mine
		}
		sent += s.fanOut(ctx, body, mine)
	}
	if s.Notify != nil {
		s.Notify(claim.Version)
	}
	return sent, nil
}

// SendTest pushes one notification to every subscribed device. The settings
// area calls it, because the only convincing proof that push works is a
// notification on the screen.
func (s *Sender) SendTest(ctx context.Context) (int, error) {
	return s.SendTestTo(ctx, "")
}

// SendTestTo pushes one notification to the devices of one account. An empty
// account means every device in the file.
func (s *Sender) SendTestTo(ctx context.Context, accountID string) (int, error) {
	now := s.Now()
	subs, err := s.Store.SubscriptionsFor(now, accountID)
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	body, err := json.Marshal(notification{
		Title: "teha", Body: "Notifications work on this device.",
		Tag: "teha-test", URL: "/", Kind: "test",
	})
	if err != nil {
		return 0, err
	}
	return s.fanOut(ctx, body, subs), nil
}

// SendEvents tells people what somebody else did: a task given to them, or a
// comment on a task they can see.
//
// This is not a reminder. A reminder is claimed in the database so that it
// fires at most once (D-010), because it fires from a timer that can run
// twice. An event has already happened once, inside one transaction, so there
// is nothing to claim: this call sends and forgets.
//
// The events are grouped by person, so one account's devices are read once
// even when a batch assigned five tasks at once.
func (s *Sender) SendEvents(ctx context.Context, events []store.Event) (int, error) {
	now := s.Now()
	subs := map[string][]store.PushSubscription{}
	var sent int
	for _, ev := range events {
		if ev.AccountID == "" {
			continue
		}
		mine, seen := subs[ev.AccountID]
		if !seen {
			var err error
			mine, err = s.Store.SubscriptionsFor(now, ev.AccountID)
			if err != nil {
				return sent, err
			}
			subs[ev.AccountID] = mine
		}
		if len(mine) == 0 {
			continue
		}
		body, ok := s.eventPayload(ev)
		if !ok {
			continue
		}
		sent += s.fanOut(ctx, body, mine)
	}
	return sent, nil
}

// eventPayload writes the words for one event.
//
// The tag holds the task and the kind, so five comments on one task collapse
// into one line in the tray and an assignment stays beside them.
func (s *Sender) eventPayload(ev store.Event) ([]byte, bool) {
	who := ev.Actor
	if who == "" {
		who = "Somebody"
	}
	title := ev.Title
	if title == "" {
		title = "a task"
	}
	n := notification{
		Tag:    "teha-" + ev.Kind + "-" + ev.TaskID,
		Kind:   ev.Kind,
		TaskID: ev.TaskID,
		URL:    "/?task=" + ev.TaskID,
	}
	switch ev.Kind {
	case store.EventAssigned:
		n.Title = title
		n.Body = who + " gave this to you"
	case store.EventCommented:
		n.Title = title
		n.Body = who + ": " + shorten(ev.Body, 120)
	default:
		return nil, false
	}
	body, err := json.Marshal(n)
	if err != nil {
		s.Log.Error("cannot write a notification", "err", err)
		return nil, false
	}
	return body, true
}

// shorten cuts a long comment to one line of a notification. A tray shows two
// lines at most, and the whole text is one tap away.
func shorten(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max]) + "..."
}

// fanOut sends one message to every subscription, a few at a time.
func (s *Sender) fanOut(ctx context.Context, body []byte, subs []store.PushSubscription) int {
	parallel := s.Parallel
	if parallel < 1 {
		parallel = 1
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		sent int
		gate = make(chan struct{}, parallel)
	)
	for _, sub := range subs {
		wg.Add(1)
		go func(sub store.PushSubscription) {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			if s.sendOne(ctx, body, sub) {
				mu.Lock()
				sent++
				mu.Unlock()
			}
		}(sub)
	}
	wg.Wait()
	return sent
}

// sendOne sends to one device and acts on the answer.
//
// The rules come from RFC 8030 and from what the browser vendors actually
// return:
//
//   - 404 or 410: the subscription is dead for good. Delete the row. Keeping it
//     means every later pass pays a round trip for a device that will never
//     answer again.
//   - 429: the push service asks for a pause. Honour Retry-After, and park the
//     subscription on disk so a restart honours it too.
//   - anything else that fails: count it and carry on. One broken device must
//     never stop the others.
func (s *Sender) sendOne(ctx context.Context, body []byte, sub store.PushSubscription) bool {
	sendCtx, cancel := context.WithTimeout(ctx, s.SendTimeout)
	defer cancel()

	// Each send gets its own copy of the message. webpush-go v1.4.0 wraps the
	// slice it is handed in a bytes.Buffer and then appends the padding
	// delimiter to it, so two concurrent sends of one slice write into the same
	// backing array. `go test -race` catches it, and one device would corrupt
	// another device's message. The copy is a few hundred bytes and it removes
	// the whole class of problem.
	msg := append([]byte(nil), body...)

	res, err := webpush.SendNotificationWithContext(sendCtx, msg, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		HTTPClient:      s.Client,
		Subscriber:      s.Keys.Subject,
		VAPIDPublicKey:  s.Keys.Public,
		VAPIDPrivateKey: s.Keys.Private,
		TTL:             s.TTL,
		Urgency:         webpush.UrgencyHigh,
	})
	if err != nil {
		s.Log.Warn("a push failed", "endpoint", endpointHost(sub.Endpoint), "err", err)
		_ = s.Store.SubscriptionFailed(sub.Endpoint)
		return false
	}
	// Read and close, so the connection returns to the pool instead of leaking.
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	retryAfter := res.Header.Get("Retry-After")
	code := res.StatusCode
	res.Body.Close()

	switch {
	case code == http.StatusNotFound || code == http.StatusGone:
		s.Log.Info("a device unsubscribed itself, so its subscription is gone",
			"endpoint", endpointHost(sub.Endpoint), "code", code)
		if err := s.Store.DeleteSubscription(sub.Endpoint); err != nil {
			s.Log.Error("cannot delete a dead subscription", "err", err)
		}
		return false
	case code == http.StatusTooManyRequests:
		wait := parseRetryAfter(retryAfter, s.Now(), time.Minute)
		s.Log.Warn("the push service asked for a pause", "wait", wait.String(),
			"endpoint", endpointHost(sub.Endpoint))
		if err := s.Store.BackOffSubscription(sub.Endpoint, s.Now().Add(wait)); err != nil {
			s.Log.Error("cannot record a back-off", "err", err)
		}
		return false
	case code >= 200 && code < 300:
		_ = s.Store.SubscriptionSent(sub.Endpoint, s.Now())
		return true
	default:
		s.Log.Warn("the push service refused a message", "code", code,
			"endpoint", endpointHost(sub.Endpoint))
		_ = s.Store.SubscriptionFailed(sub.Endpoint)
		return false
	}
}

// parseRetryAfter reads the header in both forms RFC 9110 allows: a number of
// seconds, or an HTTP date. An absent or unreadable value falls back to def,
// and nothing waits longer than MaxBackOff.
func parseRetryAfter(v string, now time.Time, def time.Duration) time.Duration {
	wait := def
	v = strings.TrimSpace(v)
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		wait = time.Duration(secs) * time.Second
	} else if when, err := http.ParseTime(v); err == nil {
		wait = when.Sub(now)
	}
	if wait < time.Second {
		wait = time.Second
	}
	if wait > MaxBackOff {
		wait = MaxBackOff
	}
	return wait
}

// endpointHost keeps a log line short and keeps the subscription secret out of
// it. The full endpoint is a capability: whoever holds it can push to the
// device, so it belongs in the database and nowhere else.
func endpointHost(endpoint string) string {
	if i := strings.Index(endpoint, "://"); i >= 0 {
		rest := endpoint[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return "unknown"
}

// --- the message ------------------------------------------------------------

// notification is what the service worker receives. The field names are the
// contract with sw.js.
type notification struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Tag    string `json:"tag"`
	URL    string `json:"url"`
	TaskID string `json:"task_id,omitempty"`
	Kind   string `json:"kind"`
}

// payload writes the message for one reminder. The second result is false when
// there is nothing worth saying.
func (s *Sender) payload(d store.DueReminder, now time.Time) ([]byte, bool) {
	n := notification{
		// The tag collapses two notifications with the same name into one in the
		// tray. The reminder id is the tag, so even a duplicate push after a
		// point-in-time restore shows the person one notification.
		Tag:    "teha-" + d.ID,
		Kind:   d.Kind,
		TaskID: d.TaskID,
		URL:    "/",
	}
	switch d.Kind {
	case store.KindDailyDigest:
		count, titles, err := s.Store.DigestSummaryFor(now.Format("2006-01-02"), 3, d.AccountID)
		if err != nil {
			s.Log.Error("cannot build the digest", "err", err)
			return nil, false
		}
		if count == 0 {
			return nil, false
		}
		n.Title = "Today"
		n.Body = plural(count, "task", "tasks") + " due"
		if len(titles) > 0 {
			n.Body += ": " + strings.Join(titles, ", ")
			if count > len(titles) {
				n.Body += fmt.Sprintf(" and %d more", count-len(titles))
			}
		}
	case store.KindBeforeDue:
		n.Title = d.Title
		n.Body = "Due in " + minutes(d.OffsetMin)
		if d.DueTime != "" {
			n.Body += ", at " + d.DueTime
		}
		n.URL = "/?task=" + d.TaskID
	default:
		n.Title = d.Title
		n.Body = "Due now"
		if d.DueTime != "" {
			n.Body = "Due at " + d.DueTime
		}
		n.URL = "/?task=" + d.TaskID
	}
	body, err := json.Marshal(n)
	if err != nil {
		s.Log.Error("cannot write a notification", "err", err)
		return nil, false
	}
	return body, true
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// minutes says a span the way a person says it.
func minutes(m int) string {
	switch {
	case m <= 0:
		return "a moment"
	case m < 60:
		return plural(m, "minute", "minutes")
	case m%60 == 0 && m < 60*24:
		return plural(m/60, "hour", "hours")
	case m%(60*24) == 0:
		return plural(m/(60*24), "day", "days")
	default:
		return plural(m/60, "hour", "hours")
	}
}
