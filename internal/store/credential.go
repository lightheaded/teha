// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"crypto/rand"
	"database/sql"
)

// OwnerID is the fixed id of the one account this build holds. A second
// account belongs to milestone M6.
const OwnerID = "owner"

// Account is one person who signs in. The user handle is the opaque identifier
// that an authenticator stores beside a passkey, so it must never change.
type Account struct {
	ID          string `json:"id"`
	UserHandle  []byte `json:"-"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

// Credential is one passkey. The store keeps the fields a login must verify
// and the fields the owner reads in a list.
//
// The public key stays in this package and never reaches a client.
type Credential struct {
	ID                string  `json:"id"`
	AccountID         string  `json:"-"`
	PublicKey         []byte  `json:"-"`
	SignCount         int64   `json:"sign_count"`
	Transports        string  `json:"transports"`
	AAGUID            string  `json:"aaguid"`
	Name              string  `json:"name"`
	Flags             int64   `json:"-"`
	AttestationType   string  `json:"-"`
	AttestationFormat string  `json:"-"`
	CreatedAt         string  `json:"created_at"`
	LastUsedAt        *string `json:"last_used_at,omitempty"`
}

// seedOwner writes the one account of this build, with a fresh user handle.
//
// The handle is 32 random bytes. The specification allows 64, and it asks for a
// value that carries no meaning, because an authenticator stores it and a
// person can read it off a lost device.
func (s *Store) seedOwner() error {
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM account WHERE id = ?`, OwnerID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	handle := make([]byte, 32)
	if _, err := rand.Read(handle); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO account (id, user_handle, name, display_name, created_at)
		VALUES (?,?,?,?,?)`, OwnerID, handle, "owner", "teha owner", s.stamp())
	return err
}

// Owner returns the account this build holds.
func (s *Store) Owner() (Account, error) {
	var a Account
	rows, err := s.db.Query(`SELECT id, user_handle, name, display_name, created_at
		FROM account WHERE id = ?`, OwnerID)
	if err != nil {
		return a, err
	}
	defer rows.Close()
	if !rows.Next() {
		return a, ErrNotFound
	}
	err = rows.Scan(&a.ID, &a.UserHandle, &a.Name, &a.DisplayName, &a.CreatedAt)
	return a, err
}

// AccountByUserHandle finds the account an authenticator names. A login reads
// the handle out of the assertion, so an unknown handle must not resolve to
// the owner by accident.
func (s *Store) AccountByUserHandle(handle []byte) (Account, error) {
	var a Account
	if len(handle) == 0 {
		return a, ErrNotFound
	}
	rows, err := s.db.Query(`SELECT id, user_handle, name, display_name, created_at
		FROM account WHERE user_handle = ?`, handle)
	if err != nil {
		return a, err
	}
	defer rows.Close()
	if !rows.Next() {
		return a, ErrNotFound
	}
	err = rows.Scan(&a.ID, &a.UserHandle, &a.Name, &a.DisplayName, &a.CreatedAt)
	return a, err
}

const credentialCols = `id, account_id, public_key, sign_count, transports, aaguid, name,
	flags, attestation_type, attestation_format, created_at, last_used_at`

func scanCredential(rows *sql.Rows) (Credential, error) {
	var c Credential
	err := rows.Scan(&c.ID, &c.AccountID, &c.PublicKey, &c.SignCount, &c.Transports, &c.AAGUID,
		&c.Name, &c.Flags, &c.AttestationType, &c.AttestationFormat, &c.CreatedAt, &c.LastUsedAt)
	return c, err
}

// AddCredential stores one passkey. A repeated credential id is an error, so a
// second enrolment of the same key cannot reset its signature counter.
func (s *Store) AddCredential(c Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.AccountID == "" {
		c.AccountID = OwnerID
	}
	if c.CreatedAt == "" {
		c.CreatedAt = s.stamp()
	}
	_, err := s.db.Exec(`INSERT INTO credential (`+credentialCols+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.AccountID, c.PublicKey, c.SignCount, c.Transports, c.AAGUID, c.Name,
		c.Flags, c.AttestationType, c.AttestationFormat, c.CreatedAt, c.LastUsedAt)
	return err
}

// Credentials returns every passkey of one account, oldest first.
func (s *Store) Credentials(accountID string) ([]Credential, error) {
	rows, err := s.db.Query(`SELECT `+credentialCols+` FROM credential
		WHERE account_id = ? ORDER BY created_at, id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Credential returns one passkey by its credential id.
func (s *Store) Credential(id string) (Credential, error) {
	rows, err := s.db.Query(`SELECT `+credentialCols+` FROM credential WHERE id = ?`, id)
	if err != nil {
		return Credential{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Credential{}, ErrNotFound
	}
	return scanCredential(rows)
}

// TouchCredential records a successful login: the new signature counter, the
// new flag byte and the time of use.
func (s *Store) TouchCredential(id string, signCount int64, flags int64, at string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE credential SET sign_count = ?, flags = ?, last_used_at = ?
		WHERE id = ?`, signCount, flags, at, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCredential removes one passkey. It reports ErrNotFound when no row
// carries that id, so a caller cannot learn a credential id by its answer.
func (s *Store) DeleteCredential(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM credential WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
