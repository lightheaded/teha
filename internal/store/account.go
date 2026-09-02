// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/lightheaded/teha/id"
)

// The household: the owner, and every person the owner invites.
//
// One file holds one household. Every row of the account hangs off a project,
// so visibility is one question: may this account see this project? An account
// sees a project it owns, and a project somebody shared with it. Nothing else.
//
// The owner is not special in the data. The owner holds the first account row
// and the only account that can write an invitation, and that is the whole of
// it. A file that has never been shared holds one account and behaves exactly
// as it did before this table existed.

// ErrDenied says that the account may not see or write the row it named. It is
// deliberately the same answer for both, so a refusal never tells a caller
// that a row it cannot see exists.
var ErrDenied = errors.New("this account cannot reach that row")

// ErrBadInvite covers every reason an invitation does not work. One message
// again, so a guess learns nothing.
var ErrBadInvite = errors.New("that invitation is not valid")

// InviteTTL is how long an invitation stays open. Long enough to send it and
// have the other person type it in, short enough that a forgotten one closes.
const InviteTTL = 7 * 24 * time.Hour

// Account is one person in the household.
type Account struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
	// InboxID names the project that a capture with no project lands in. Each
	// account has one, because an inbox is the most private list there is.
	InboxID string `json:"inbox_id"`
	IsOwner bool   `json:"is_owner"`
	// UserHandle is the WebAuthn user handle. It never leaves the server.
	UserHandle []byte `json:"-"`
}

// --- secrets ----------------------------------------------------------------

// secretHash hashes a device token, an invitation code or a session value.
//
// A plain SHA-256 is right here and Argon2id is not. Every one of these
// secrets is 160 random bits that this server generated, so there is no
// dictionary to run and no work factor worth paying on every request. A
// password would be the other case, and this build has none. See D-009.
func secretHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// newSecret returns a fresh secret in base32 with no padding: 160 bits, and no
// character that a person can misread when they type it from a phone screen.
func newSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)), nil
}

// --- accounts ---------------------------------------------------------------

const accountCols = `id, user_handle, name, display_name, created_at,
	coalesce(inbox_id, ?), coalesce(is_owner, 0)`

func scanAccount(rows *sql.Rows) (Account, error) {
	var a Account
	var owner int
	err := rows.Scan(&a.ID, &a.UserHandle, &a.Name, &a.DisplayName, &a.CreatedAt, &a.InboxID, &owner)
	a.IsOwner = owner == 1
	return a, err
}

// Accounts lists the household, the owner first.
func (s *Store) Accounts() ([]Account, error) {
	rows, err := s.db.Query(`SELECT `+accountCols+` FROM account ORDER BY is_owner DESC, created_at`, InboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AccountByID returns one account.
func (s *Store) AccountByID(accountID string) (Account, error) {
	rows, err := s.db.Query(`SELECT `+accountCols+` FROM account WHERE id = ?`, InboxID, accountID)
	if err != nil {
		return Account{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Account{}, ErrNotFound
	}
	return scanAccount(rows)
}

// SetAccountToken writes the device token of one account. An empty token takes
// the token away, which is how a device is locked out without deleting the
// person.
func (s *Store) SetAccountToken(accountID, token string) error {
	hash := ""
	if token != "" {
		hash = secretHash(token)
	}
	res, err := s.db.Exec(`UPDATE account SET token_hash = ? WHERE id = ?`, hash, accountID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AccountForToken finds the account that a device token belongs to.
//
// The lookup is by hash, so the comparison is a primary key read and not a
// scan of every account with a timing-safe compare. The hash of a wrong token
// simply matches no row.
func (s *Store) AccountForToken(token string) (Account, error) {
	if token == "" {
		return Account{}, ErrNotFound
	}
	rows, err := s.db.Query(`SELECT `+accountCols+` FROM account WHERE token_hash = ? AND token_hash <> ''`,
		InboxID, secretHash(token))
	if err != nil {
		return Account{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Account{}, ErrNotFound
	}
	return scanAccount(rows)
}

// --- sessions ---------------------------------------------------------------

// NewSession opens a session for one account and returns the cookie value. The
// value is never stored: the table holds its hash.
func (s *Store) NewSession(accountID string, ttl time.Duration) (string, error) {
	value, err := newSecret()
	if err != nil {
		return "", err
	}
	now := s.Now().UTC()
	_, err = s.db.Exec(`INSERT INTO session (id, account_id, created_at, last_seen_at, expires_at)
		VALUES (?,?,?,?,?)`, secretHash(value), accountID,
		now.Format(time.RFC3339), now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	return value, nil
}

// SessionAccount returns the account of a live session, and marks it seen. An
// expired session is deleted rather than reported, so the table cannot grow
// without limit on a device that never signs out.
func (s *Store) SessionAccount(value string) (Account, error) {
	if value == "" {
		return Account{}, ErrNotFound
	}
	key := secretHash(value)
	var accountID, expires string
	err := s.db.QueryRow(`SELECT account_id, expires_at FROM session WHERE id = ?`, key).Scan(&accountID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	at, err := time.Parse(time.RFC3339, expires)
	if err != nil || !s.Now().UTC().Before(at) {
		_, _ = s.db.Exec(`DELETE FROM session WHERE id = ?`, key)
		return Account{}, ErrNotFound
	}
	_, _ = s.db.Exec(`UPDATE session SET last_seen_at = ? WHERE id = ?`, s.stamp(), key)
	return s.AccountByID(accountID)
}

// DeleteSession signs one browser out.
func (s *Store) DeleteSession(value string) error {
	if value == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM session WHERE id = ?`, secretHash(value))
	return err
}

// PruneSessions removes what has expired.
func (s *Store) PruneSessions() error {
	_, err := s.db.Exec(`DELETE FROM session WHERE expires_at <= ?`, s.stamp())
	return err
}

// --- invitations ------------------------------------------------------------

// Invite is one invitation into the household. Code is filled in only by
// CreateInvite, and only that once.
type Invite struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Code      string  `json:"code,omitempty"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt string  `json:"expires_at"`
	UsedAt    *string `json:"used_at,omitempty"`
	UsedBy    *string `json:"used_by,omitempty"`
}

// CreateInvite writes one invitation and returns it with its code. Only the
// hash of the code is kept, so a lost code is replaced and never recovered.
func (s *Store) CreateInvite(by, name string, ttl time.Duration) (Invite, error) {
	code, err := newSecret()
	if err != nil {
		return Invite{}, err
	}
	now := s.Now().UTC()
	inv := Invite{
		ID:        id.New("i"),
		Name:      strings.TrimSpace(name),
		Code:      code,
		CreatedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(ttl).Format(time.RFC3339),
	}
	if inv.Name == "" {
		return Invite{}, errors.New("an invitation needs a name, so that two of them can be told apart")
	}
	_, err = s.db.Exec(`INSERT INTO invite (id, code_hash, name, created_by, created_at, expires_at)
		VALUES (?,?,?,?,?,?)`, inv.ID, secretHash(code), inv.Name, by, inv.CreatedAt, inv.ExpiresAt)
	if err != nil {
		return Invite{}, err
	}
	return inv, nil
}

// Invites lists what the owner has written, newest first, with no codes.
func (s *Store) Invites() ([]Invite, error) {
	rows, err := s.db.Query(`SELECT id, name, created_at, expires_at, used_at, used_by
		FROM invite ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var i Invite
		if err := rows.Scan(&i.ID, &i.Name, &i.CreatedAt, &i.ExpiresAt, &i.UsedAt, &i.UsedBy); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// RevokeInvite deletes an invitation that nobody used.
func (s *Store) RevokeInvite(inviteID string) error {
	_, err := s.db.Exec(`DELETE FROM invite WHERE id = ? AND used_at IS NULL`, inviteID)
	return err
}

// RedeemInvite turns a code into a new account with its own inbox and its own
// device token. It returns the account and the token, and the token is shown
// once.
//
// The whole thing is one transaction. A half-made account with no inbox would
// take every capture of that person to a project that does not exist.
func (s *Store) RedeemInvite(code, displayName string) (Account, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return Account{}, "", err
	}
	defer tx.Rollback()

	var inviteID, expires string
	var used *string
	err = tx.QueryRow(`SELECT id, expires_at, used_at FROM invite WHERE code_hash = ?`,
		secretHash(code)).Scan(&inviteID, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, "", ErrBadInvite
	}
	if err != nil {
		return Account{}, "", err
	}
	if used != nil {
		return Account{}, "", ErrBadInvite
	}
	at, err := time.Parse(time.RFC3339, expires)
	if err != nil || !s.Now().UTC().Before(at) {
		return Account{}, "", ErrBadInvite
	}

	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "someone"
	}
	accountID := id.New("a")
	inboxID := "inbox_" + accountID
	handle := make([]byte, 32)
	if _, err := rand.Read(handle); err != nil {
		return Account{}, "", err
	}
	token, err := newSecret()
	if err != nil {
		return Account{}, "", err
	}
	now := s.stamp()

	if _, err := tx.Exec(`INSERT INTO account
		(id, user_handle, name, display_name, created_at, token_hash, inbox_id, is_owner)
		VALUES (?,?,?,?,?,?,?,0)`,
		accountID, handle, name, name, now, secretHash(token), inboxID); err != nil {
		return Account{}, "", err
	}

	// The inbox of the new account is a project like any other, owned by them.
	v, err := bump(tx, "project", inboxID, "insert", now)
	if err != nil {
		return Account{}, "", err
	}
	if _, err := tx.Exec(`INSERT INTO project
		(id, name, color, parent_id, order_key, is_inbox, owner_id, created_at, updated_at, version)
		VALUES (?,?,?,NULL,?,1,?,?,?,?)`,
		inboxID, "Inbox", "grey", "m", accountID, now, now, v); err != nil {
		return Account{}, "", err
	}

	if _, err := tx.Exec(`UPDATE invite SET used_at = ?, used_by = ? WHERE id = ?`,
		now, accountID, inviteID); err != nil {
		return Account{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, "", err
	}
	return Account{ID: accountID, Name: name, DisplayName: name, CreatedAt: now, InboxID: inboxID}, token, nil
}

// --- sharing ----------------------------------------------------------------

// visibleProjects is the sub-select of every project id one account may see.
// The account id goes in twice, once for the owner test and once for the
// membership test.
const visibleProjects = `SELECT id FROM project WHERE owner_id = ?
	UNION SELECT project_id FROM project_member WHERE account_id = ?`

// ShareProject gives one account sight of a project. Only the owner of the
// project may call it, and the caller checks that.
func (s *Store) ShareProject(projectID, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withMembershipChange(accountID, func(tx *sql.Tx, now string) error {
		_, err := tx.Exec(`INSERT OR IGNORE INTO project_member (project_id, account_id, created_at)
			VALUES (?,?,?)`, projectID, accountID, now)
		return err
	})
}

// UnshareProject takes it away again.
func (s *Store) UnshareProject(projectID, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withMembershipChange(accountID, func(tx *sql.Tx, now string) error {
		_, err := tx.Exec(`DELETE FROM project_member WHERE project_id = ? AND account_id = ?`,
			projectID, accountID)
		return err
	})
}

// withMembershipChange runs a change to what one account may see, and records
// the version it happened at.
//
// A scoped pull cannot say that a row went away: it simply stops sending it,
// and the client keeps what it already has. The record here lets the next pull
// answer "your view changed, start again", which is one full pull and no
// guessing. See Delta.Reset.
func (s *Store) withMembershipChange(accountID string, fn func(tx *sql.Tx, now string) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.stamp()
	if err := fn(tx, now); err != nil {
		return err
	}
	// The version comes from change_log, so one counter orders every event a
	// client can see, a membership change included.
	v, err := bump(tx, "project_member", accountID, "update", now)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO membership_change (version, account_id, at) VALUES (?,?,?)`,
		v, accountID, now); err != nil {
		return err
	}
	return tx.Commit()
}

// ProjectMembers lists the accounts a project is shared with, the owner apart.
func (s *Store) ProjectMembers(projectID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT account_id FROM project_member WHERE project_id = ? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Shares returns every share in the household, as a map of project id to the
// accounts that hold it. The web app draws the sharing panel from this.
func (s *Store) Shares() (map[string][]string, error) {
	rows, err := s.db.Query(`SELECT project_id, account_id FROM project_member ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var p, a string
		if err := rows.Scan(&p, &a); err != nil {
			return nil, err
		}
		out[p] = append(out[p], a)
	}
	return out, rows.Err()
}

// ProjectOwner returns the account that owns a project.
func (s *Store) ProjectOwner(projectID string) (string, error) {
	var owner sql.NullString
	err := s.db.QueryRow(`SELECT owner_id FROM project WHERE id = ?`, projectID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !owner.Valid || owner.String == "" {
		// A project from a file written before the household existed belongs
		// to the owner. migrate() fills these in, so this is the belt.
		return OwnerID, nil
	}
	return owner.String, nil
}

// CanSee reports whether an account may read a project.
func (s *Store) CanSee(accountID, projectID string) (bool, error) {
	return canSee(s.db, accountID, projectID)
}

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func canSee(q querier, accountID, projectID string) (bool, error) {
	var n int
	err := q.QueryRow(`SELECT count(*) FROM project WHERE id = ? AND (owner_id = ?
		OR id IN (SELECT project_id FROM project_member WHERE account_id = ?))`,
		projectID, accountID, accountID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// membershipMoved reports whether what this account may see changed after the
// version a client last pulled.
func (s *Store) membershipMoved(accountID string, since int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM membership_change WHERE account_id = ? AND version > ?`,
		accountID, since).Scan(&n)
	return n > 0, err
}
