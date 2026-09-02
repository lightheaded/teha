// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"database/sql"
	"fmt"
	"time"
)

// The three kinds of reminder. A kind changes what the notification says and
// how long a late notification stays useful.
const (
	KindAtDue       = "at_due"
	KindBeforeDue   = "before_due"
	KindDailyDigest = "daily_digest"
)

// Reminder is one notification at one moment. It is account data: it carries a
// version, it goes into the change log, and it reaches every client through
// Pull, exactly like a task.
type Reminder struct {
	ID        string  `json:"id"`
	TaskID    *string `json:"task_id,omitempty"`
	Kind      string  `json:"kind"`
	FireAt    string  `json:"fire_at"`
	OffsetMin *int    `json:"offset_min,omitempty"`
	SentAt    *string `json:"sent_at,omitempty"`
	DeletedAt *string `json:"deleted_at,omitempty"`
	Version   int64   `json:"v"`
}

const reminderCols = `id, task_id, kind, fire_at, offset_min, sent_at, deleted_at, version`

func scanReminder(rows *sql.Rows) (Reminder, error) {
	var r Reminder
	err := rows.Scan(&r.ID, &r.TaskID, &r.Kind, &r.FireAt, &r.OffsetMin, &r.SentAt, &r.DeletedAt, &r.Version)
	return r, err
}

// GraceWindow says how late a reminder of this kind can be and still fire.
//
// The server was down when the reminder came due. A reminder that arrives six
// hours late is not a reminder, it is noise, and noise teaches a person to
// ignore the next one. A reminder that arrives four minutes late after a
// restart is exactly what the person wanted. So a late reminder fires once
// inside the window and never outside it. See docs/DECISIONS.md D-011.
//
// A digest gets a longer window than a point reminder, because a digest of
// today is still worth reading at 11:00. A "call the garage at 09:00" at 14:00
// is not.
func GraceWindow(kind string) time.Duration {
	switch kind {
	case KindDailyDigest:
		return 4 * time.Hour
	default:
		return time.Hour
	}
}

// --- commands ---------------------------------------------------------------

// ReminderArgs carries the fields of a reminder. A nil pointer means "leave
// alone" on an update, the same rule TaskArgs follows.
type ReminderArgs struct {
	ID        string  `json:"id"`
	TaskID    *string `json:"task_id,omitempty"`
	Kind      *string `json:"kind,omitempty"`
	FireAt    *string `json:"fire_at,omitempty"`
	OffsetMin *int    `json:"offset_min,omitempty"`
}

func reminderAdd(tx *sql.Tx, a ReminderArgs, now string, act actor) (string, error) {
	if a.ID == "" {
		return "", fmt.Errorf("reminder_add needs a client id")
	}
	kind := KindAtDue
	if a.Kind != nil {
		kind = *a.Kind
	}
	if err := checkKind(kind); err != nil {
		return "", err
	}
	if a.FireAt == nil {
		return "", fmt.Errorf("reminder_add needs fire_at")
	}
	fireAt, err := normalTime(*a.FireAt)
	if err != nil {
		return "", err
	}
	if kind == KindDailyDigest {
		if a.TaskID != nil && *a.TaskID != "" {
			return "", fmt.Errorf("a daily digest belongs to the account, not to a task")
		}
	} else if a.TaskID == nil || *a.TaskID == "" {
		return "", fmt.Errorf("a %s reminder needs a task_id", kind)
	}
	v, err := bump(tx, "reminder", a.ID, "insert", now)
	if err != nil {
		return "", err
	}
	// A reminder belongs to the person who set it, not to the task. Two people
	// who share a chore each keep their own nudge, or none.
	_, err = tx.Exec(`INSERT INTO reminder (id, task_id, kind, fire_at, offset_min,
		account_id, created_at, updated_at, version) VALUES (?,?,?,?,?,?,?,?,?)`,
		a.ID, a.TaskID, kind, fireAt, a.OffsetMin, act.ID, now, now, v)
	return a.ID, err
}

func reminderUpdate(tx *sql.Tx, a ReminderArgs, now string) error {
	set := map[string]any{}
	if a.Kind != nil {
		if err := checkKind(*a.Kind); err != nil {
			return err
		}
		set["kind"] = *a.Kind
	}
	if a.FireAt != nil {
		fireAt, err := normalTime(*a.FireAt)
		if err != nil {
			return err
		}
		set["fire_at"] = fireAt
		// A new moment re-arms the reminder. The claim predicate compares
		// sent_at with fire_at, so a moment after the last send is claimable
		// again without any extra column.
	}
	if a.OffsetMin != nil {
		set["offset_min"] = *a.OffsetMin
	}
	if len(set) == 0 {
		return fmt.Errorf("reminder_update has nothing to change")
	}
	return rowSet(tx, "reminder", a.ID, now, set)
}

func checkKind(kind string) error {
	switch kind {
	case KindAtDue, KindBeforeDue, KindDailyDigest:
		return nil
	}
	return fmt.Errorf("unknown reminder kind %q: use at_due, before_due or daily_digest", kind)
}

// normalTime accepts an RFC 3339 moment in any zone and stores it in UTC, so
// every comparison in SQL is a string comparison over one zone.
func normalTime(v string) (string, error) {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return "", fmt.Errorf("fire_at must be an RFC 3339 moment such as 2026-08-27T07:00:00Z: %w", err)
	}
	return t.UTC().Format(time.RFC3339), nil
}

// --- reads ------------------------------------------------------------------

// Reminders returns every live reminder of one task, oldest moment first.
func (s *Store) Reminders(taskID string) ([]Reminder, error) {
	rows, err := s.db.Query(`SELECT `+reminderCols+` FROM reminder
		WHERE task_id = ? AND deleted_at IS NULL ORDER BY fire_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reminder{}
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- the claim --------------------------------------------------------------

// DueReminder is one reminder that must go out now, with the task fields the
// notification needs.
type DueReminder struct {
	ID string
	// AccountID is the person who set it. The sender pushes to that person's
	// devices and to nobody else's.
	AccountID string
	Kind      string
	TaskID    string
	Title     string
	DueDate   string
	DueTime   string
	Priority  int
	OffsetMin int
	FireAt    time.Time
}

// Claim reports what one pass of the scheduler took.
type Claim struct {
	Due     []DueReminder // send these, once
	Skipped int           // came due too long ago, marked and dropped
	Version int64         // the account version after the claim
}

// ClaimDue marks every due reminder as sent and returns the ones to send.
//
// The order matters and it is the whole once-only guarantee. The read and the
// mark happen in ONE transaction, and the transaction commits BEFORE any push
// leaves the process. So:
//
//   - A crash between the commit and the send loses one notification. The task
//     is still on the list, so the person still sees the work.
//   - A crash cannot produce a second notification, on this run or on the next
//     one, because the claim predicate is already false for the row.
//
// At most once, deliberately. A duplicate reminder is worse than a missing one:
// it teaches the person that the notification means nothing. See D-010.
func (s *Store) ClaimDue(now time.Time, limit int) (Claim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out Claim
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	nowUTC := now.UTC().Format(time.RFC3339)
	stamp := s.stamp()

	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	// A point reminder of a task that is closed or deleted is not due at all.
	// The person acted already, and a notification then is only noise.
	rows, err := tx.Query(`SELECT r.id, coalesce(r.account_id,'`+OwnerID+`'), r.kind, r.fire_at,
			coalesce(r.task_id,''), coalesce(r.offset_min,0),
			coalesce(t.title,''), coalesce(t.due_date,''), coalesce(t.due_time,''), coalesce(t.priority,4)
		FROM reminder r LEFT JOIN task t ON t.id = r.task_id
		WHERE r.deleted_at IS NULL
		  AND r.fire_at <= ?
		  AND (r.sent_at IS NULL OR r.sent_at < r.fire_at)
		  AND (r.kind = ? OR (t.id IS NOT NULL AND t.state = 'open' AND t.deleted_at IS NULL))
		ORDER BY r.fire_at LIMIT ?`, nowUTC, KindDailyDigest, limit)
	if err != nil {
		return out, err
	}
	type claimed struct {
		d      DueReminder
		inTime bool
	}
	var all []claimed
	for rows.Next() {
		var d DueReminder
		var fireAt string
		if err := rows.Scan(&d.ID, &d.AccountID, &d.Kind, &fireAt, &d.TaskID, &d.OffsetMin,
			&d.Title, &d.DueDate, &d.DueTime, &d.Priority); err != nil {
			rows.Close()
			return out, err
		}
		t, err := time.Parse(time.RFC3339, fireAt)
		if err != nil {
			rows.Close()
			return out, fmt.Errorf("reminder %s holds a bad fire_at %q: %w", d.ID, fireAt, err)
		}
		d.FireAt = t
		all = append(all, claimed{d: d, inTime: now.Sub(t) <= GraceWindow(d.Kind)})
	}
	err = rows.Err()
	// The pool holds one connection, so an open result set blocks every write
	// that follows.
	rows.Close()
	if err != nil {
		return out, err
	}

	for _, c := range all {
		set := map[string]any{"sent_at": stamp}
		if c.d.Kind == KindDailyDigest {
			// The digest is the one recurring kind. It moves one day forward in
			// the same transaction, so it is claimable again tomorrow and never
			// twice for the same day. The step is from the old moment, not from
			// now, so a late pass does not drift the clock time.
			set["fire_at"] = c.d.FireAt.Add(24 * time.Hour).UTC().Format(time.RFC3339)
		}
		if err := rowSet(tx, "reminder", c.d.ID, stamp, set); err != nil {
			return out, err
		}
		if c.inTime {
			out.Due = append(out.Due, c.d)
		} else {
			out.Skipped++
		}
	}

	var v sql.NullInt64
	if err := tx.QueryRow(`SELECT max(version) FROM change_log`).Scan(&v); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	out.Version = v.Int64
	return out, nil
}

// DigestSummary counts the open tasks due on or before day and names the first
// few, so a digest notification says something useful in one line.
func (s *Store) DigestSummary(day string, names int) (int, []string, error) {
	return s.DigestSummaryFor(day, names, OwnerID)
}

// DigestSummaryFor counts what one account has due, and names the first few.
// A digest that counted the whole household would tell each person how much
// work the other one has.
func (s *Store) DigestSummaryFor(day string, names int, accountID string) (int, []string, error) {
	if accountID == "" {
		accountID = OwnerID
	}
	mine := ` AND project_id IN (SELECT id FROM project WHERE owner_id = ?
		UNION SELECT project_id FROM project_member WHERE account_id = ?)`
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM task
		WHERE state = 'open' AND deleted_at IS NULL AND due_date IS NOT NULL AND due_date <= ?`+mine,
		day, accountID, accountID).Scan(&n)
	if err != nil {
		return 0, nil, err
	}
	if n == 0 || names <= 0 {
		return n, nil, nil
	}
	rows, err := s.db.Query(`SELECT title FROM task
		WHERE state = 'open' AND deleted_at IS NULL AND due_date IS NOT NULL AND due_date <= ?`+mine+`
		ORDER BY due_date, priority LIMIT ?`, day, accountID, accountID, names)
	if err != nil {
		return n, nil, err
	}
	defer rows.Close()
	var titles []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return n, titles, err
		}
		titles = append(titles, t)
	}
	return n, titles, rows.Err()
}

// --- push subscriptions -----------------------------------------------------

// PushSubscription is one browser or one installed web app. It is device
// plumbing, not account data, so it stays out of sync and out of the change
// log.
type PushSubscription struct {
	// AccountID is the person whose browser this is. An empty value means the
	// owner, which is what a file written before the household had.
	AccountID  string `json:"-"`
	Endpoint   string `json:"endpoint"`
	P256dh     string `json:"p256dh"`
	Auth       string `json:"auth"`
	UserAgent  string `json:"user_agent,omitempty"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	FailCount  int    `json:"fail_count"`
}

// SaveSubscription stores one subscription. The endpoint is the identity, so a
// browser that re-subscribes replaces its own row instead of growing a second
// one. A re-subscribe also clears the back-off and the failure count: the
// device just told us it is alive.
func (s *Store) SaveSubscription(sub PushSubscription) error {
	if sub.Endpoint == "" || sub.P256dh == "" || sub.Auth == "" {
		return fmt.Errorf("a subscription needs an endpoint, a p256dh key and an auth secret")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.stamp()
	account := sub.AccountID
	if account == "" {
		account = OwnerID
	}
	_, err := s.db.Exec(`INSERT INTO push_subscription
			(endpoint, p256dh, auth, user_agent, account_id, created_at, fail_count, retry_until)
			VALUES (?,?,?,?,?,?,0,NULL)
		ON CONFLICT(endpoint) DO UPDATE SET
			p256dh = excluded.p256dh, auth = excluded.auth,
			user_agent = excluded.user_agent, account_id = excluded.account_id,
			fail_count = 0, retry_until = NULL`,
		sub.Endpoint, sub.P256dh, sub.Auth, sub.UserAgent, account, now)
	return err
}

// Subscriptions returns every subscription that is not inside a back-off.
func (s *Store) Subscriptions(now time.Time) ([]PushSubscription, error) {
	return s.SubscriptionsFor(now, "")
}

// SubscriptionsFor returns the devices of one account. An empty account means
// every device in the file, which is what a test and a one-account file want.
func (s *Store) SubscriptionsFor(now time.Time, accountID string) ([]PushSubscription, error) {
	nowUTC := now.UTC().Format(time.RFC3339)
	where := `WHERE (retry_until IS NULL OR retry_until <= ?)`
	args := []any{nowUTC}
	if accountID != "" {
		where += ` AND coalesce(account_id, ?) = ?`
		args = append(args, OwnerID, accountID)
	}
	rows, err := s.db.Query(`SELECT endpoint, p256dh, auth, user_agent, created_at,
			coalesce(last_used_at,''), fail_count
		FROM push_subscription `+where+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PushSubscription{}
	for rows.Next() {
		var p PushSubscription
		if err := rows.Scan(&p.Endpoint, &p.P256dh, &p.Auth, &p.UserAgent, &p.CreatedAt,
			&p.LastUsedAt, &p.FailCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CountSubscriptions returns how many devices are subscribed, back-off or not.
func (s *Store) CountSubscriptions() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM push_subscription`).Scan(&n)
	return n, err
}

// DeleteSubscription removes one subscription. A 404 or a 410 from the push
// service means the device is gone for good, so the row goes with it.
func (s *Store) DeleteSubscription(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM push_subscription WHERE endpoint = ?`, endpoint)
	return err
}

// SubscriptionSent records a delivery and clears the failure state.
func (s *Store) SubscriptionSent(endpoint string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE push_subscription
		SET last_used_at = ?, fail_count = 0, retry_until = NULL WHERE endpoint = ?`,
		at.UTC().Format(time.RFC3339), endpoint)
	return err
}

// BackOffSubscription parks one subscription until a moment. A 429 from a push
// service asks for exactly this, and the deadline is on disk, so a restart
// still honours it.
func (s *Store) BackOffSubscription(endpoint string, until time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE push_subscription SET retry_until = ?, fail_count = fail_count + 1
		WHERE endpoint = ?`, until.UTC().Format(time.RFC3339), endpoint)
	return err
}

// SubscriptionFailed counts one failure that is neither dead nor a back-off.
func (s *Store) SubscriptionFailed(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE push_subscription SET fail_count = fail_count + 1 WHERE endpoint = ?`, endpoint)
	return err
}

// RetryUntil returns the back-off deadline of one subscription, or an empty
// string. It exists for the tests and for the settings answer.
func (s *Store) RetryUntil(endpoint string) (string, error) {
	var v sql.NullString
	err := s.db.QueryRow(`SELECT retry_until FROM push_subscription WHERE endpoint = ?`, endpoint).Scan(&v)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return v.String, err
}
