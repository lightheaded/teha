// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openCredStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "cred.db"))
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return fixed }
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOwnerHasAStableUserHandle(t *testing.T) {
	s := openCredStore(t)
	a, err := s.Owner()
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != OwnerID {
		t.Errorf("owner id is %q, want %q", a.ID, OwnerID)
	}
	if len(a.UserHandle) != 32 {
		t.Fatalf("the user handle is %d bytes, want 32", len(a.UserHandle))
	}
	// A second read gives the same handle. An authenticator stores it beside
	// the passkey, so a new handle would orphan every credential.
	b, err := s.Owner()
	if err != nil {
		t.Fatal(err)
	}
	if string(a.UserHandle) != string(b.UserHandle) {
		t.Error("the user handle changed between two reads")
	}
	if _, err := s.AccountByUserHandle(a.UserHandle); err != nil {
		t.Errorf("the owner is not found by the handle: %v", err)
	}
	if _, err := s.AccountByUserHandle([]byte("not a handle")); err == nil {
		t.Error("an unknown handle resolved to an account")
	}
	if _, err := s.AccountByUserHandle(nil); err == nil {
		t.Error("an empty handle resolved to an account")
	}
}

func TestCredentialRoundTrip(t *testing.T) {
	s := openCredStore(t)
	row := Credential{
		ID:                "Y3JlZC1vbmU",
		AccountID:         OwnerID,
		PublicKey:         []byte{1, 2, 3, 4},
		SignCount:         7,
		Transports:        "internal,hybrid",
		AAGUID:            "adce0002-35bc-c60a-648b-0b25f1f05503",
		Name:              "Phone",
		Flags:             5,
		AttestationType:   "none",
		AttestationFormat: "none",
	}
	if err := s.AddCredential(row); err != nil {
		t.Fatal(err)
	}

	got, err := s.Credential(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Phone" || got.SignCount != 7 || got.Transports != "internal,hybrid" {
		t.Errorf("the row came back as %+v", got)
	}
	if string(got.PublicKey) != string(row.PublicKey) {
		t.Errorf("the public key came back as %v", got.PublicKey)
	}
	if got.AAGUID != row.AAGUID {
		t.Errorf("the AAGUID came back as %q", got.AAGUID)
	}
	if got.Flags != 5 {
		t.Errorf("the flag byte came back as %d, want 5", got.Flags)
	}
	if got.CreatedAt != "2026-08-27T10:00:00Z" {
		t.Errorf("created_at is %q", got.CreatedAt)
	}
	if got.LastUsedAt != nil {
		t.Errorf("a fresh credential reports a last use: %v", *got.LastUsedAt)
	}

	list, err := s.Credentials(OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("the list holds %d rows, want 1", len(list))
	}

	// The same credential must not enrol twice. A second row would carry a
	// fresh signature counter for the same authenticator.
	if err := s.AddCredential(row); err == nil {
		t.Error("the same credential id was stored twice")
	}

	if err := s.TouchCredential(row.ID, 11, 9, "2026-08-27T11:00:00Z"); err != nil {
		t.Fatal(err)
	}
	got, err = s.Credential(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SignCount != 11 || got.Flags != 9 {
		t.Errorf("the counter and the flags are %d and %d, want 11 and 9", got.SignCount, got.Flags)
	}
	if got.LastUsedAt == nil || *got.LastUsedAt != "2026-08-27T11:00:00Z" {
		t.Errorf("last_used_at is %v", got.LastUsedAt)
	}

	if err := s.TouchCredential("no such id", 1, 0, "2026-08-27T11:00:00Z"); err != ErrNotFound {
		t.Errorf("touching an unknown credential returned %v, want ErrNotFound", err)
	}
	if err := s.DeleteCredential(row.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Credential(row.ID); err != ErrNotFound {
		t.Errorf("the deleted credential returned %v, want ErrNotFound", err)
	}
	if err := s.DeleteCredential(row.ID); err != ErrNotFound {
		t.Errorf("a second delete returned %v, want ErrNotFound", err)
	}
}

// A credential is not account data. It must never reach a client through the
// sync delta, and it must not move the version counter.
func TestACredentialDoesNotChangeTheSyncVersion(t *testing.T) {
	s := openCredStore(t)
	before, err := s.Version()
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddCredential(Credential{ID: "Y3JlZC10d28", PublicKey: []byte{9}, Name: "Laptop"})
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.Version()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("the version moved from %d to %d", before, after)
	}
	d, err := s.Pull(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 0 {
		t.Errorf("the delta carries %d tasks", len(d.Tasks))
	}
}
