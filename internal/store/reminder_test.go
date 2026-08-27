// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"path/filepath"
	"testing"
	"time"
)

// The clock every reminder test counts from. No test here reads the wall
// clock, so a run at midnight behaves like a run at noon.
var base = time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)

// newStoreAt opens a store at a named path, so a test can close it and open the
// same file again. That is how a restart is simulated.
func newStoreAt(t *testing.T, path string, now time.Time) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return now }
	return s
}

func addTask(t *testing.T, s *Store, id, title, due string) {
	t.Helper()
	_, res, err := s.Apply([]Command{
		cmd(t, "add-"+id, "task_add", TaskArgs{ID: id, Title: ptr(title), DueDate: ptr(due)}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res[0].OK {
		t.Fatalf("task_add %s failed: %s", id, res[0].Error)
	}
}

func addReminder(t *testing.T, s *Store, id, taskID, kind string, fireAt time.Time) {
	t.Helper()
	a := ReminderArgs{ID: id, Kind: ptr(kind), FireAt: ptr(fireAt.Format(time.RFC3339))}
	if taskID != "" {
		a.TaskID = ptr(taskID)
	}
	_, res, err := s.Apply([]Command{cmd(t, "rem-"+id, "reminder_add", a)})
	if err != nil {
		t.Fatal(err)
	}
	if !res[0].OK {
		t.Fatalf("reminder_add %s failed: %s", id, res[0].Error)
	}
}

func ids(due []DueReminder) []string {
	out := make([]string, 0, len(due))
	for _, d := range due {
		out = append(out, d.ID)
	}
	return out
}

func TestClaimDueSelectsOnlyWhatIsActuallyDue(t *testing.T) {
	s := newStore(t)
	s.Now = func() time.Time { return base }

	addTask(t, s, "t1", "Call the garage", "2026-08-27")
	addTask(t, s, "t2", "Book the ferry", "2026-08-27")
	addTask(t, s, "t3", "Order gravel", "2026-08-27")

	addReminder(t, s, "r1", "t1", KindAtDue, base.Add(-10*time.Minute)) // due
	addReminder(t, s, "r2", "t2", KindAtDue, base.Add(time.Hour))       // not yet
	addReminder(t, s, "r3", "t3", KindAtDue, base.Add(-time.Minute))    // due, but the task closes below
	addReminder(t, s, "r4", "", KindDailyDigest, base)                  // due, no task

	// A completed task needs no reminder. The person acted already.
	if _, res, err := s.Apply([]Command{cmd(t, "done-t3", "task_complete", IDArgs{ID: "t3"})}); err != nil || !res[0].OK {
		t.Fatalf("cannot complete t3: %v %+v", err, res)
	}

	claim, err := s.ClaimDue(base, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := ids(claim.Due)
	if len(got) != 2 || got[0] != "r1" || got[1] != "r4" {
		t.Fatalf("want r1 and r4, got %v", got)
	}
	if claim.Skipped != 0 {
		t.Fatalf("nothing was late, so want 0 skipped, got %d", claim.Skipped)
	}
	if claim.Due[0].Title != "Call the garage" {
		t.Fatalf("the claim must carry the task title, got %q", claim.Due[0].Title)
	}

	// A deleted reminder never fires.
	if _, res, err := s.Apply([]Command{cmd(t, "del-r2", "reminder_delete", IDArgs{ID: "r2"})}); err != nil || !res[0].OK {
		t.Fatalf("cannot delete r2: %v %+v", err, res)
	}
	claim, err = s.ClaimDue(base.Add(2*time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.Due) != 0 || claim.Skipped != 0 {
		t.Fatalf("a deleted reminder must not fire, got %v and %d skipped", ids(claim.Due), claim.Skipped)
	}
}

// A reminder must never fire twice, and a restart must not change that. The
// claim marks the row in the same transaction that reads it, so the guarantee
// survives anything that happens after the commit. See D-010.
func TestClaimIsOnceOnlyAcrossARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")

	s := newStoreAt(t, path, base)
	addTask(t, s, "t1", "Call the garage", "2026-08-27")
	addReminder(t, s, "r1", "t1", KindAtDue, base.Add(-5*time.Minute))

	claim, err := s.ClaimDue(base, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.Due) != 1 {
		t.Fatalf("the first pass must claim one reminder, got %v", ids(claim.Due))
	}

	// The process stops here, before any push left the process. Everything the
	// claim wrote is committed.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	again := newStoreAt(t, path, base.Add(time.Minute))
	defer again.Close()
	claim, err = again.ClaimDue(base.Add(time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.Due) != 0 || claim.Skipped != 0 {
		t.Fatalf("after a restart the reminder must not come back, got %v and %d skipped",
			ids(claim.Due), claim.Skipped)
	}
}

// The server was down when the reminder came due. Inside the window it fires
// late, once. Outside the window it is marked and dropped. See D-011.
func TestAMissedReminderFiresLateOnlyInsideItsWindow(t *testing.T) {
	cases := []struct {
		name  string
		kind  string
		late  time.Duration
		fires bool
	}{
		{"a point reminder 30 minutes late", KindAtDue, 30 * time.Minute, true},
		{"a point reminder 2 hours late", KindAtDue, 2 * time.Hour, false},
		{"a before-due reminder 59 minutes late", KindBeforeDue, 59 * time.Minute, true},
		{"a digest 3 hours late", KindDailyDigest, 3 * time.Hour, true},
		{"a digest 9 hours late", KindDailyDigest, 9 * time.Hour, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStore(t)
			s.Now = func() time.Time { return base }
			taskID := ""
			if c.kind != KindDailyDigest {
				addTask(t, s, "t1", "Call the garage", "2026-08-27")
				taskID = "t1"
			}
			addReminder(t, s, "r1", taskID, c.kind, base.Add(-c.late))

			claim, err := s.ClaimDue(base, 100)
			if err != nil {
				t.Fatal(err)
			}
			if c.fires && len(claim.Due) != 1 {
				t.Fatalf("want one late reminder to fire, got %v", ids(claim.Due))
			}
			if !c.fires && (len(claim.Due) != 0 || claim.Skipped != 1) {
				t.Fatalf("want the reminder dropped, got %v and %d skipped", ids(claim.Due), claim.Skipped)
			}

			// Either way the row is finished. A dropped reminder must not come
			// back on the next pass and fire even later.
			claim, err = s.ClaimDue(base.Add(time.Minute), 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(claim.Due) != 0 || claim.Skipped != 0 {
				t.Fatalf("the second pass must find nothing, got %v and %d skipped",
					ids(claim.Due), claim.Skipped)
			}
		})
	}
}

// The digest is the one recurring kind. One claim per day, and the claim moves
// the moment forward by exactly one day.
func TestTheDigestFiresOncePerDay(t *testing.T) {
	s := newStore(t)
	s.Now = func() time.Time { return base }
	addReminder(t, s, "d1", "", KindDailyDigest, base)

	claim, err := s.ClaimDue(base, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.Due) != 1 {
		t.Fatalf("the digest must fire at its moment, got %v", ids(claim.Due))
	}

	// Six more passes on the same day change nothing.
	for i := 1; i <= 6; i++ {
		at := base.Add(time.Duration(i) * time.Hour)
		s.Now = func() time.Time { return at }
		claim, err = s.ClaimDue(at, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(claim.Due) != 0 {
			t.Fatalf("pass %d on the same day fired again: %v", i, ids(claim.Due))
		}
	}

	tomorrow := base.Add(24 * time.Hour)
	s.Now = func() time.Time { return tomorrow }
	claim, err = s.ClaimDue(tomorrow, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.Due) != 1 {
		t.Fatalf("the digest must fire again tomorrow, got %v", ids(claim.Due))
	}
	if got := claim.Due[0].FireAt; !got.Equal(tomorrow) {
		t.Fatalf("want the moment %s, got %s", tomorrow, got)
	}
}

func TestDigestSummaryCountsWhatIsDue(t *testing.T) {
	s := newStore(t)
	s.Now = func() time.Time { return base }
	addTask(t, s, "t1", "Call the garage", "2026-08-26") // overdue
	addTask(t, s, "t2", "Book the ferry", "2026-08-27")  // today
	addTask(t, s, "t3", "Order gravel", "2026-09-01")    // later

	n, titles, err := s.DigestSummary("2026-08-27", 3)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want two tasks due today or earlier, got %d", n)
	}
	if len(titles) != 2 || titles[0] != "Call the garage" {
		t.Fatalf("want the overdue task first, got %v", titles)
	}
}

func TestReminderTravelsInSync(t *testing.T) {
	s := newStore(t)
	s.Now = func() time.Time { return base }
	v0, _ := s.Version()
	addTask(t, s, "t1", "Call the garage", "2026-08-27")
	addReminder(t, s, "r1", "t1", KindBeforeDue, base.Add(time.Hour))

	d, err := s.Pull(v0)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Reminders) != 1 {
		t.Fatalf("want one reminder in the delta, got %d", len(d.Reminders))
	}
	r := d.Reminders[0]
	if r.ID != "r1" || r.Kind != KindBeforeDue || r.TaskID == nil || *r.TaskID != "t1" {
		t.Fatalf("the delta carries the wrong reminder: %+v", r)
	}
	if r.Version == 0 {
		t.Fatal("a reminder must carry a version, like every other row")
	}
	if r.SentAt != nil {
		t.Fatalf("a new reminder is not sent yet, got %v", *r.SentAt)
	}

	// The sent marker is a change like any other, so the next pull carries it.
	before := r.Version
	if _, err := s.ClaimDue(base.Add(time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	d, err = s.Pull(before)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Reminders) != 1 || d.Reminders[0].SentAt == nil {
		t.Fatalf("a client must learn that the reminder fired, got %+v", d.Reminders)
	}
}

func TestReminderRefusesNonsense(t *testing.T) {
	s := newStore(t)
	s.Now = func() time.Time { return base }
	addTask(t, s, "t1", "Call the garage", "2026-08-27")

	cases := []struct {
		name string
		args ReminderArgs
	}{
		{"an unknown kind", ReminderArgs{ID: "x1", TaskID: ptr("t1"), Kind: ptr("whenever"), FireAt: ptr(base.Format(time.RFC3339))}},
		{"no moment", ReminderArgs{ID: "x2", TaskID: ptr("t1"), Kind: ptr(KindAtDue)}},
		{"a moment that is not a moment", ReminderArgs{ID: "x3", TaskID: ptr("t1"), Kind: ptr(KindAtDue), FireAt: ptr("tomorrow")}},
		{"a point reminder with no task", ReminderArgs{ID: "x4", Kind: ptr(KindAtDue), FireAt: ptr(base.Format(time.RFC3339))}},
		{"a digest tied to a task", ReminderArgs{ID: "x5", TaskID: ptr("t1"), Kind: ptr(KindDailyDigest), FireAt: ptr(base.Format(time.RFC3339))}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, res, err := s.Apply([]Command{cmd(t, "bad-"+c.args.ID, "reminder_add", c.args)})
			if err != nil {
				t.Fatal(err)
			}
			if res[0].OK {
				t.Fatal("the command must fail and say why")
			}
		})
	}
}

func TestReminderKeepsItsMomentInUTC(t *testing.T) {
	s := newStore(t)
	s.Now = func() time.Time { return base }
	addTask(t, s, "t1", "Call the garage", "2026-08-27")
	zone := time.FixedZone("EEST", 3*3600)
	local := time.Date(2026, 8, 27, 12, 0, 0, 0, zone)
	addReminder(t, s, "r1", "t1", KindAtDue, local)

	rs, err := s.Reminders("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].FireAt != "2026-08-27T09:00:00Z" {
		t.Fatalf("want the moment stored in UTC, got %+v", rs)
	}
}

func TestSubscriptionsUpsertAndBackOff(t *testing.T) {
	s := newStore(t)
	s.Now = func() time.Time { return base }
	sub := PushSubscription{Endpoint: "https://push.example/abc", P256dh: "key", Auth: "secret", UserAgent: "Firefox on Android"}
	if err := s.SaveSubscription(sub); err != nil {
		t.Fatal(err)
	}
	sub.P256dh = "key2"
	if err := s.SaveSubscription(sub); err != nil {
		t.Fatal(err)
	}
	n, err := s.CountSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("one device is one row, got %d", n)
	}
	got, err := s.Subscriptions(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].P256dh != "key2" {
		t.Fatalf("a re-subscribe replaces the keys, got %+v", got)
	}

	if err := s.BackOffSubscription(sub.Endpoint, base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err = s.Subscriptions(base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("a subscription inside its back-off must not be sent to")
	}
	got, err = s.Subscriptions(base.Add(3 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatal("the back-off must end")
	}

	if err := s.DeleteSubscription(sub.Endpoint); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountSubscriptions(); n != 0 {
		t.Fatalf("want no subscription left, got %d", n)
	}
}
