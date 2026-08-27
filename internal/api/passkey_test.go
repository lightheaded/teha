// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"

	"github.com/lightheaded/teha/internal/store"
)

const (
	testHost   = "teha.example"
	testRPID   = "teha.example"
	testOrigin = "https://teha.example"
	testToken  = "secret"
)

// --- a browser that keeps its cookies ---------------------------------------

// agent is a tiny browser. It carries the cookies the server sets, and it
// sends a Host header of its own, so the tests can drive the relying-party
// rules and the cookie attributes that follow from the host.
type agent struct {
	t       *testing.T
	ts      *httptest.Server
	host    string
	token   string
	cookies map[string]string
	last    []*http.Cookie
	// xff sets X-Forwarded-For, so a test can act as many client addresses.
	xff string
}

func newAgent(t *testing.T, ts *httptest.Server) *agent {
	return &agent{t: t, ts: ts, host: testHost, cookies: map[string]string{}}
}

func (a *agent) do(method, path, body string) (int, []byte) {
	a.t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, a.ts.URL+path, r)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Host = a.host
	req.Header.Set("Content-Type", "application/json")
	if a.xff != "" {
		req.Header.Set("X-Forwarded-For", a.xff)
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	for name, value := range a.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	res, err := a.ts.Client().Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	a.last = res.Cookies()
	for _, c := range a.last {
		if c.MaxAge < 0 || c.Value == "" {
			delete(a.cookies, c.Name)
			continue
		}
		a.cookies[c.Name] = c.Value
	}
	return res.StatusCode, out
}

func (a *agent) setCookie(res []*http.Cookie, name string) *http.Cookie {
	a.t.Helper()
	for _, c := range res {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// --- a software authenticator ------------------------------------------------

// authenticator is a stub of the real thing: one ECDSA key, one credential id
// and a signature counter the test can drive. It builds the same two bodies a
// browser posts, so every check the server makes runs against real bytes.
type authenticator struct {
	t          *testing.T
	key        *ecdsa.PrivateKey
	credID     []byte
	userHandle []byte
	rpID       string
	origin     string
	// verifyUser reports the UV flag. A false value is what a passkey looks
	// like when nobody proved they were present with a biometric or a PIN.
	verifyUser bool
}

func newAuthenticator(t *testing.T, handle []byte) *authenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		t.Fatal(err)
	}
	return &authenticator{
		t: t, key: key, credID: id, userHandle: handle,
		rpID: testRPID, origin: testOrigin, verifyUser: true,
	}
}

func (a *authenticator) coseKey() []byte {
	a.t.Helper()
	pub := a.key.PublicKey
	data, err := webauthncbor.Marshal(map[int64]any{
		1:  int64(webauthncose.EllipticKey),
		3:  int64(webauthncose.AlgES256),
		-1: int64(webauthncose.P256),
		-2: pub.X.FillBytes(make([]byte, 32)),
		-3: pub.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		a.t.Fatal(err)
	}
	return data
}

func (a *authenticator) flags(attested bool) protocol.AuthenticatorFlags {
	f := protocol.FlagUserPresent
	if a.verifyUser {
		f |= protocol.FlagUserVerified
	}
	if attested {
		f |= protocol.FlagAttestedCredentialData
	}
	return f
}

func (a *authenticator) authData(attested bool, signCount uint32) []byte {
	hash := sha256.Sum256([]byte(a.rpID))
	out := append([]byte{}, hash[:]...)
	out = append(out, byte(a.flags(attested)))
	out = binary.BigEndian.AppendUint32(out, signCount)
	if !attested {
		return out
	}
	key := a.coseKey()
	out = append(out, make([]byte, 16)...) // an AAGUID of zero: no model claimed
	out = binary.BigEndian.AppendUint16(out, uint16(len(a.credID)))
	out = append(out, a.credID...)
	return append(out, key...)
}

func (a *authenticator) clientData(ceremony, challenge string) []byte {
	a.t.Helper()
	data, err := json.Marshal(map[string]any{
		"type":        ceremony,
		"challenge":   challenge,
		"origin":      a.origin,
		"crossOrigin": false,
	})
	if err != nil {
		a.t.Fatal(err)
	}
	return data
}

func (a *authenticator) sign(authData, clientData []byte) []byte {
	a.t.Helper()
	clientHash := sha256.Sum256(clientData)
	signed := append(append([]byte{}, authData...), clientHash[:]...)
	digest := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		a.t.Fatal(err)
	}
	return sig
}

// attestation builds the body of a registration answer, with no attestation
// statement. This build asks for none, because it runs no metadata service.
func (a *authenticator) attestation(challenge string) string {
	a.t.Helper()
	authData := a.authData(true, 0)
	clientData := a.clientData("webauthn.create", challenge)
	object, err := webauthncbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	})
	if err != nil {
		a.t.Fatal(err)
	}
	id := base64.RawURLEncoding.EncodeToString(a.credID)
	body, err := json.Marshal(map[string]any{
		"id": id, "rawId": id, "type": "public-key",
		"response": map[string]any{
			"attestationObject": base64.RawURLEncoding.EncodeToString(object),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
		},
	})
	if err != nil {
		a.t.Fatal(err)
	}
	return string(body)
}

// assertion builds the body of a login answer at one signature counter.
func (a *authenticator) assertion(challenge string, signCount uint32) string {
	a.t.Helper()
	authData := a.authData(false, signCount)
	clientData := a.clientData("webauthn.get", challenge)
	id := base64.RawURLEncoding.EncodeToString(a.credID)
	body, err := json.Marshal(map[string]any{
		"id": id, "rawId": id, "type": "public-key",
		"response": map[string]any{
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
			"signature":         base64.RawURLEncoding.EncodeToString(a.sign(authData, clientData)),
			"userHandle":        base64.RawURLEncoding.EncodeToString(a.userHandle),
		},
	})
	if err != nil {
		a.t.Fatal(err)
	}
	return string(body)
}

// --- the harness -------------------------------------------------------------

type harness struct {
	srv  *Server
	ts   *httptest.Server
	now  time.Time
	auth *authenticator
}

func newPasskeyHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "passkey.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	h := &harness{now: time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)}
	st.Now = func() time.Time { return h.now }

	s := New(st, testToken, slog.New(slog.DiscardHandler))
	s.Now = func() time.Time { return h.now }
	// A test pins the relying party, so no test depends on the address the
	// test server happened to bind. The defaults have their own test below.
	s.RP = RelyingParty{ID: testRPID, Origin: testOrigin, DisplayName: "teha"}
	h.srv = s
	h.ts = httptest.NewServer(s.Routes())
	t.Cleanup(h.ts.Close)

	owner, err := st.Owner()
	if err != nil {
		t.Fatal(err)
	}
	h.auth = newAuthenticator(t, owner.UserHandle)
	return h
}

// challengeOf reads the challenge out of a begin answer.
func challengeOf(t *testing.T, body []byte) string {
	t.Helper()
	var out struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("cannot read the begin answer: %v: %s", err, body)
	}
	if out.PublicKey.Challenge == "" {
		t.Fatalf("the begin answer carries no challenge: %s", body)
	}
	return out.PublicKey.Challenge
}

// enrol runs a whole registration with the device token and returns the agent.
func (h *harness) enrol(t *testing.T, name string) *agent {
	t.Helper()
	a := newAgent(t, h.ts)
	a.token = testToken
	code, body := a.do("POST", "/v1/passkeys/register/begin", "{}")
	if code != http.StatusOK {
		t.Fatalf("register/begin returned %d: %s", code, body)
	}
	challenge := challengeOf(t, body)
	code, body = a.do("POST", "/v1/passkeys/register/finish?name="+name, h.auth.attestation(challenge))
	if code != http.StatusOK {
		t.Fatalf("register/finish returned %d: %s", code, body)
	}
	return a
}

// signIn runs a login with no token at all and returns the answer.
func (h *harness) signIn(t *testing.T, a *agent, signCount uint32) (int, []byte) {
	t.Helper()
	code, body := a.do("POST", "/v1/passkeys/login/begin", "{}")
	if code != http.StatusOK {
		return code, body
	}
	challenge := challengeOf(t, body)
	return a.do("POST", "/v1/passkeys/login/finish", h.auth.assertion(challenge, signCount))
}

// --- registration behind the token ------------------------------------------

func TestRegistrationNeedsTheDeviceToken(t *testing.T) {
	h := newPasskeyHarness(t)
	for _, path := range []string{"/v1/passkeys/register/begin", "/v1/passkeys/register/finish"} {
		a := newAgent(t, h.ts)
		if code, _ := a.do("POST", path, "{}"); code != http.StatusUnauthorized {
			t.Errorf("%s without a token returned %d, want 401", path, code)
		}
		a.token = "wrong"
		if code, _ := a.do("POST", path, "{}"); code != http.StatusUnauthorized {
			t.Errorf("%s with a wrong token returned %d, want 401", path, code)
		}
	}
	// The right token opens the door.
	a := newAgent(t, h.ts)
	a.token = testToken
	if code, body := a.do("POST", "/v1/passkeys/register/begin", "{}"); code != http.StatusOK {
		t.Fatalf("register/begin with the token returned %d: %s", code, body)
	}
}

// A passkey session is not an invitation. It reads and deletes credentials, and
// it cannot enrol another one.
func TestASessionCannotEnrolAPasskey(t *testing.T) {
	h := newPasskeyHarness(t)
	a := h.enrol(t, "Phone")
	a.token = "" // drop the bearer header
	delete(a.cookies, "teha_token")
	if code, body := h.signIn(t, a, 1); code != http.StatusOK {
		t.Fatalf("the login returned %d: %s", code, body)
	}
	if code, _ := a.do("POST", "/v1/passkeys/register/begin", "{}"); code != http.StatusUnauthorized {
		t.Errorf("a session began a registration and returned %d, want 401", code)
	}
	// The same session reads the list, so the guard difference is deliberate.
	if code, body := a.do("GET", "/v1/passkeys", ""); code != http.StatusOK {
		t.Errorf("a session could not list the passkeys: %d: %s", code, body)
	}
}

func TestRegisterAndSignInWithAPasskey(t *testing.T) {
	h := newPasskeyHarness(t)
	h.enrol(t, "Phone")

	rows, err := h.srv.Store.Credentials(store.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("the store holds %d credentials, want 1", len(rows))
	}
	if rows[0].Name != "Phone" {
		t.Errorf("the credential is named %q, want Phone", rows[0].Name)
	}
	if rows[0].CreatedAt != "2026-08-27T09:00:00Z" {
		t.Errorf("created_at is %q, so the injected clock is not in use", rows[0].CreatedAt)
	}

	// A fresh browser with no token signs in with the passkey alone.
	b := newAgent(t, h.ts)
	if code, body := h.signIn(t, b, 1); code != http.StatusOK {
		t.Fatalf("the login returned %d: %s", code, body)
	}
	if b.cookies[sessionCookieName] == "" {
		t.Fatal("the login set no session cookie")
	}
	// The session opens the ordinary API, which is the point of the cookie.
	if code, body := b.do("GET", "/v1/tasks", ""); code != http.StatusOK {
		t.Errorf("the session could not read the tasks: %d: %s", code, body)
	}
	// The counter and the last use are recorded.
	got, err := h.srv.Store.Credential(rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SignCount != 1 {
		t.Errorf("the signature counter is %d, want 1", got.SignCount)
	}
	if got.LastUsedAt == nil || *got.LastUsedAt != "2026-08-27T09:00:00Z" {
		t.Errorf("last_used_at is %v", got.LastUsedAt)
	}
}

// --- the cookie --------------------------------------------------------------

func TestSessionCookieAttributesAndLifetime(t *testing.T) {
	h := newPasskeyHarness(t)
	a := h.enrol(t, "Phone")
	a.token = ""
	delete(a.cookies, "teha_token")
	if code, body := h.signIn(t, a, 1); code != http.StatusOK {
		t.Fatalf("the login returned %d: %s", code, body)
	}
	c := a.setCookie(a.last, sessionCookieName)
	if c == nil {
		t.Fatal("the login set no session cookie")
	}
	if !c.Secure {
		t.Error("the session cookie is not Secure")
	}
	if !c.HttpOnly {
		t.Error("the session cookie is not HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("the session cookie has SameSite %v, want Lax", c.SameSite)
	}
	if c.Name == "teha_token" {
		t.Error("the session reuses the device token cookie")
	}
	wantAge := int(DefaultSessionTTL.Seconds())
	if c.MaxAge != wantAge {
		t.Errorf("the session cookie lives %d seconds, want %d", c.MaxAge, wantAge)
	}
	if !c.Expires.Equal(h.now.Add(DefaultSessionTTL)) {
		t.Errorf("the session expires at %v, want %v", c.Expires, h.now.Add(DefaultSessionTTL))
	}

	// The session works now, and it is dead one second after its lifetime.
	if code, _ := a.do("GET", "/v1/tasks", ""); code != http.StatusOK {
		t.Error("the fresh session did not open the API")
	}
	h.now = h.now.Add(DefaultSessionTTL + time.Second)
	if code, _ := a.do("GET", "/v1/tasks", ""); code != http.StatusUnauthorized {
		t.Errorf("an expired session returned %d, want 401", code)
	}
}

func TestLogoutClearsTheSession(t *testing.T) {
	h := newPasskeyHarness(t)
	a := h.enrol(t, "Phone")
	a.token = ""
	delete(a.cookies, "teha_token")
	if code, body := h.signIn(t, a, 1); code != http.StatusOK {
		t.Fatalf("the login returned %d: %s", code, body)
	}
	value := a.cookies[sessionCookieName]
	if code, body := a.do("POST", "/v1/logout", ""); code != http.StatusOK {
		t.Fatalf("logout returned %d: %s", code, body)
	}
	if c := a.setCookie(a.last, sessionCookieName); c == nil || c.MaxAge >= 0 {
		t.Error("logout did not expire the session cookie in the browser")
	}
	// The server forgot it too, so a copy of the cookie is worthless.
	a.cookies[sessionCookieName] = value
	if code, _ := a.do("GET", "/v1/tasks", ""); code != http.StatusUnauthorized {
		t.Errorf("the cookie still worked after logout: %d", code)
	}
}

// --- the failures that must fail closed --------------------------------------

func TestLoginFailsOnAWrongOrigin(t *testing.T) {
	h := newPasskeyHarness(t)
	a := h.enrol(t, "Phone")
	a.token = ""
	delete(a.cookies, "teha_token")
	h.auth.origin = "https://evil.example"
	if code, body := h.signIn(t, a, 1); code != http.StatusUnauthorized {
		t.Errorf("an assertion from a wrong origin returned %d, want 401: %s", code, body)
	}
	if code, _ := a.do("GET", "/v1/tasks", ""); code != http.StatusUnauthorized {
		t.Error("the refused login still opened the API")
	}
}

// The relying-party id and the origin are pinned when the ceremony begins. A
// caller that rewrites the Host header between the two steps cannot move them.
func TestLoginFailsWhenTheHostChangesMidCeremony(t *testing.T) {
	h := newPasskeyHarness(t)
	h.srv.RP = RelyingParty{DisplayName: "teha"} // read the host from the request
	a := newAgent(t, h.ts)
	a.token = testToken
	code, body := a.do("POST", "/v1/passkeys/register/begin", "{}")
	if code != http.StatusOK {
		t.Fatalf("register/begin returned %d: %s", code, body)
	}
	code, body = a.do("POST", "/v1/passkeys/register/finish?name=Phone", h.auth.attestation(challengeOf(t, body)))
	if code != http.StatusOK {
		t.Fatalf("register/finish returned %d: %s", code, body)
	}

	b := newAgent(t, h.ts)
	code, body = b.do("POST", "/v1/passkeys/login/begin", "{}")
	if code != http.StatusOK {
		t.Fatalf("login/begin returned %d: %s", code, body)
	}
	challenge := challengeOf(t, body)
	// The attacker holds a page on another name and finishes there.
	h.auth.rpID, h.auth.origin = "evil.example", "https://evil.example"
	b.host = "evil.example"
	if code, body := b.do("POST", "/v1/passkeys/login/finish", h.auth.assertion(challenge, 1)); code != http.StatusUnauthorized {
		t.Errorf("a changed host returned %d, want 401: %s", code, body)
	}
}

func TestLoginFailsOnAReplayedCounter(t *testing.T) {
	h := newPasskeyHarness(t)
	a := h.enrol(t, "Phone")
	a.token = ""
	delete(a.cookies, "teha_token")
	if code, body := h.signIn(t, a, 5); code != http.StatusOK {
		t.Fatalf("the first login returned %d: %s", code, body)
	}
	b := newAgent(t, h.ts)
	// The same counter as the stored one. A cloned authenticator and a replayed
	// assertion both look like this.
	if code, body := h.signIn(t, b, 5); code != http.StatusUnauthorized {
		t.Errorf("a repeated counter returned %d, want 401: %s", code, body)
	}
	c := newAgent(t, h.ts)
	if code, body := h.signIn(t, c, 4); code != http.StatusUnauthorized {
		t.Errorf("a lower counter returned %d, want 401: %s", code, body)
	}
	// The stored counter did not move, so a refusal writes nothing.
	rows, err := h.srv.Store.Credentials(store.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].SignCount != 5 {
		t.Errorf("the stored counter is %d, want 5", rows[0].SignCount)
	}
	// A counter above the stored one still works, so the rule is "increase",
	// not "never sign in again".
	d := newAgent(t, h.ts)
	if code, body := h.signIn(t, d, 6); code != http.StatusOK {
		t.Errorf("a higher counter returned %d, want 200: %s", code, body)
	}
}

func TestLoginFailsOnAnUnknownCredentialID(t *testing.T) {
	h := newPasskeyHarness(t)
	h.enrol(t, "Phone")
	// A second authenticator, never enrolled, with the right user handle.
	other := newAuthenticator(t, h.auth.userHandle)
	a := newAgent(t, h.ts)
	code, body := a.do("POST", "/v1/passkeys/login/begin", "{}")
	if code != http.StatusOK {
		t.Fatalf("login/begin returned %d: %s", code, body)
	}
	code, body = a.do("POST", "/v1/passkeys/login/finish", other.assertion(challengeOf(t, body), 1))
	if code != http.StatusUnauthorized {
		t.Errorf("an unknown credential id returned %d, want 401: %s", code, body)
	}
}

// The credential id is right and the key is wrong. This is the test that
// proves the signature itself is verified, and not only the identifiers.
func TestLoginFailsOnABadSignature(t *testing.T) {
	h := newPasskeyHarness(t)
	h.enrol(t, "Phone")
	other := newAuthenticator(t, h.auth.userHandle)
	other.credID = h.auth.credID
	a := newAgent(t, h.ts)
	code, body := a.do("POST", "/v1/passkeys/login/begin", "{}")
	if code != http.StatusOK {
		t.Fatalf("login/begin returned %d: %s", code, body)
	}
	code, body = a.do("POST", "/v1/passkeys/login/finish", other.assertion(challengeOf(t, body), 1))
	if code != http.StatusUnauthorized {
		t.Errorf("a signature from another key returned %d, want 401: %s", code, body)
	}
}

// A challenge from one ceremony must not verify against another. The library
// compares them, and this test keeps that guarantee visible here.
func TestLoginFailsOnAWrongChallenge(t *testing.T) {
	h := newPasskeyHarness(t)
	h.enrol(t, "Phone")
	a := newAgent(t, h.ts)
	if code, body := a.do("POST", "/v1/passkeys/login/begin", "{}"); code != http.StatusOK {
		t.Fatalf("login/begin returned %d: %s", code, body)
	}
	wrong := base64.RawURLEncoding.EncodeToString([]byte("a challenge the server never sent"))
	code, body := a.do("POST", "/v1/passkeys/login/finish", h.auth.assertion(wrong, 1))
	if code != http.StatusUnauthorized {
		t.Errorf("a wrong challenge returned %d, want 401: %s", code, body)
	}
}

func TestLoginFailsOnAnUnknownUserHandle(t *testing.T) {
	h := newPasskeyHarness(t)
	h.enrol(t, "Phone")
	h.auth.userHandle = []byte("a handle that no account carries")
	a := newAgent(t, h.ts)
	if code, body := h.signIn(t, a, 1); code != http.StatusUnauthorized {
		t.Errorf("an unknown user handle returned %d, want 401: %s", code, body)
	}
}

// The policy of this build: user verification is required. An authenticator
// that reports no verification does not sign in.
func TestLoginFailsWithoutUserVerification(t *testing.T) {
	h := newPasskeyHarness(t)
	a := h.enrol(t, "Phone")
	a.token = ""
	delete(a.cookies, "teha_token")
	h.auth.verifyUser = false
	if code, body := h.signIn(t, a, 1); code != http.StatusUnauthorized {
		t.Errorf("an unverified assertion returned %d, want 401: %s", code, body)
	}
}

func TestRegistrationFailsWithoutUserVerification(t *testing.T) {
	h := newPasskeyHarness(t)
	h.auth.verifyUser = false
	a := newAgent(t, h.ts)
	a.token = testToken
	code, body := a.do("POST", "/v1/passkeys/register/begin", "{}")
	if code != http.StatusOK {
		t.Fatalf("register/begin returned %d: %s", code, body)
	}
	code, body = a.do("POST", "/v1/passkeys/register/finish?name=Phone", h.auth.attestation(challengeOf(t, body)))
	if code != http.StatusBadRequest {
		t.Errorf("an unverified registration returned %d, want 400: %s", code, body)
	}
	rows, err := h.srv.Store.Credentials(store.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("the refused registration wrote %d rows", len(rows))
	}
}

func TestACeremonyIsSingleUse(t *testing.T) {
	h := newPasskeyHarness(t)
	a := h.enrol(t, "Phone")
	a.token = ""
	delete(a.cookies, "teha_token")

	code, body := a.do("POST", "/v1/passkeys/login/begin", "{}")
	if code != http.StatusOK {
		t.Fatalf("login/begin returned %d: %s", code, body)
	}
	assertion := h.auth.assertion(challengeOf(t, body), 1)
	ceremonyValue := a.cookies[ceremonyCookieName]
	if ceremonyValue == "" {
		t.Fatal("login/begin set no ceremony cookie")
	}
	if code, body := a.do("POST", "/v1/passkeys/login/finish", assertion); code != http.StatusOK {
		t.Fatalf("the login returned %d: %s", code, body)
	}
	// Put the cookie back by hand and post the same body again.
	a.cookies[ceremonyCookieName] = ceremonyValue
	if code, body := a.do("POST", "/v1/passkeys/login/finish", assertion); code != http.StatusBadRequest {
		t.Errorf("a replayed ceremony returned %d, want 400: %s", code, body)
	}
}

func TestACeremonyExpires(t *testing.T) {
	h := newPasskeyHarness(t)
	a := h.enrol(t, "Phone")
	a.token = ""
	delete(a.cookies, "teha_token")
	code, body := a.do("POST", "/v1/passkeys/login/begin", "{}")
	if code != http.StatusOK {
		t.Fatalf("login/begin returned %d: %s", code, body)
	}
	assertion := h.auth.assertion(challengeOf(t, body), 1)
	h.now = h.now.Add(ceremonyTTL + time.Second)
	if code, body := a.do("POST", "/v1/passkeys/login/finish", assertion); code != http.StatusBadRequest {
		t.Errorf("an expired ceremony returned %d, want 400: %s", code, body)
	}
}

func TestFinishWithoutABeginIsRefused(t *testing.T) {
	h := newPasskeyHarness(t)
	h.enrol(t, "Phone")
	a := newAgent(t, h.ts)
	if code, _ := a.do("POST", "/v1/passkeys/login/finish", h.auth.assertion("a-challenge-nobody-issued", 1)); code != http.StatusBadRequest {
		t.Errorf("a finish with no ceremony returned %d, want 400", code)
	}
}

// The login page must not say whether the account has a passkey. A begin with
// nothing enrolled answers exactly like a begin with one enrolled, and neither
// answer names a credential.
func TestLoginBeginTellsNothingAboutTheAccount(t *testing.T) {
	h := newPasskeyHarness(t)
	a := newAgent(t, h.ts)
	code, empty := a.do("POST", "/v1/passkeys/login/begin", "{}")
	if code != http.StatusOK {
		t.Fatalf("login/begin with no passkey returned %d: %s", code, empty)
	}
	if strings.Contains(string(empty), "allowCredentials") {
		t.Errorf("the answer carries a credential list: %s", empty)
	}

	h.enrol(t, "Phone")
	b := newAgent(t, h.ts)
	code, full := b.do("POST", "/v1/passkeys/login/begin", "{}")
	if code != http.StatusOK {
		t.Fatalf("login/begin with a passkey returned %d: %s", code, full)
	}
	if strings.Contains(string(full), "allowCredentials") {
		t.Errorf("the answer carries a credential list: %s", full)
	}
	// The two answers differ in the challenge alone, so their shapes match.
	shape := func(b []byte) string {
		var out map[string]map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		keys := []string{}
		for k := range out["publicKey"] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return strings.Join(keys, ",")
	}
	if shape(empty) != shape(full) {
		t.Errorf("the two answers have different shapes: %q and %q", shape(empty), shape(full))
	}
}

// --- the lockout -------------------------------------------------------------

func TestRepeatedFailuresLockTheClientOut(t *testing.T) {
	h := newPasskeyHarness(t)
	a := h.enrol(t, "Phone")
	a.token = ""
	delete(a.cookies, "teha_token")
	h.auth.origin = "https://evil.example" // every login now fails

	for i := 0; i < ipFailureAllowance; i++ {
		if code, body := h.signIn(t, a, uint32(i+1)); code != http.StatusUnauthorized {
			t.Fatalf("failure %d returned %d, want 401: %s", i+1, code, body)
		}
	}
	// One more failure crosses the allowance and starts the lockout.
	if code, _ := h.signIn(t, a, 99); code != http.StatusUnauthorized {
		t.Fatal("the last allowed failure did not answer 401")
	}
	code, body := a.do("POST", "/v1/passkeys/login/begin", "{}")
	if code != http.StatusTooManyRequests {
		t.Fatalf("the locked out client returned %d, want 429: %s", code, body)
	}
	if a.setCookie(a.last, ceremonyCookieName) != nil {
		t.Error("a locked out client was still given a challenge")
	}

	// The lockout ends, and a good passkey works again.
	h.now = h.now.Add(lockoutMax + time.Second)
	h.auth.origin = testOrigin
	if code, body := h.signIn(t, a, 100); code != http.StatusOK {
		t.Errorf("after the lockout the login returned %d, want 200: %s", code, body)
	}
	// A login that worked clears the counter.
	if wait := h.srv.lockout(&http.Request{RemoteAddr: "1.2.3.4:1"}); wait != 0 {
		t.Errorf("the account is still locked for %v", wait)
	}
}

// A quiet period forgets the failures. An old attack must not leave the owner
// at the longest wait for ever.
func TestAQuietPeriodForgetsTheFailures(t *testing.T) {
	h := newPasskeyHarness(t)
	a := h.enrol(t, "Phone")
	a.token = ""
	delete(a.cookies, "teha_token")
	h.auth.origin = "https://evil.example"
	// One address per attempt, which is the case the account budget exists for.
	// The per-address counter never fires, so only the account budget can stop
	// this, and the proxy header is what names each address.
	h.srv.TrustForwarded = true
	for i := 0; i < accountFailureAllowance+1; i++ {
		a.xff = "198.51.100." + strconv.Itoa(i+1)
		if code, body := h.signIn(t, a, uint32(i+1)); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d returned %d, want 401: %s", i+1, code, body)
		}
	}
	a.xff = "198.51.100.200"
	if code, _ := a.do("POST", "/v1/passkeys/login/begin", "{}"); code != http.StatusTooManyRequests {
		t.Fatalf("a fresh address returned %d, want 429 from the account budget", code)
	}
	if wait := h.srv.lockout(&http.Request{RemoteAddr: "9.9.9.9:1"}); wait == 0 {
		t.Fatal("the account budget did not lock anything")
	}
	h.now = h.now.Add(2 * lockoutMax)
	if wait := h.srv.lockout(&http.Request{RemoteAddr: "9.9.9.9:1"}); wait != 0 {
		t.Errorf("the account is still locked for %v after a quiet period", wait)
	}
	h.srv.sessMu.Lock()
	n := h.srv.accountFails.n
	rows := len(h.srv.failures)
	h.srv.sessMu.Unlock()
	if n != 0 {
		t.Errorf("the account counter is %d, want 0", n)
	}
	if rows != 0 {
		t.Errorf("%d address counters survived the quiet period", rows)
	}
}

func TestLockDurationBacksOff(t *testing.T) {
	cases := []struct {
		n    int
		want time.Duration
	}{
		{1, 0}, {5, 0},
		{6, lockoutBase},
		{7, 2 * lockoutBase},
		{8, 4 * lockoutBase},
		{40, lockoutMax},
		{1000, lockoutMax},
	}
	for _, c := range cases {
		got := lockDuration(c.n, ipFailureAllowance)
		if got > lockoutMax {
			t.Errorf("%d failures lock for %v, above the cap %v", c.n, got, lockoutMax)
		}
		if c.want <= lockoutMax && got != c.want {
			t.Errorf("%d failures lock for %v, want %v", c.n, got, c.want)
		}
	}
}

// --- list and delete ---------------------------------------------------------

func TestListAndDeleteAPasskey(t *testing.T) {
	h := newPasskeyHarness(t)
	a := h.enrol(t, "Phone")

	code, body := a.do("GET", "/v1/passkeys", "")
	if code != http.StatusOK {
		t.Fatalf("the list returned %d: %s", code, body)
	}
	var list struct {
		Passkeys []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			PublicKey  string `json:"public_key"`
			SignCount  int64  `json:"sign_count"`
			LastUsedAt string `json:"last_used_at"`
		} `json:"passkeys"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Passkeys) != 1 || list.Passkeys[0].Name != "Phone" {
		t.Fatalf("the list is %s", body)
	}
	// The public key never leaves the server.
	if strings.Contains(string(body), "public_key") {
		t.Error("the list carries the public key")
	}

	id := list.Passkeys[0].ID
	if code, body := a.do("DELETE", "/v1/passkeys/"+id, ""); code != http.StatusOK {
		t.Fatalf("the delete returned %d: %s", code, body)
	}
	if code, _ := a.do("DELETE", "/v1/passkeys/"+id, ""); code != http.StatusNotFound {
		t.Errorf("a second delete returned %d, want 404", code)
	}
	// A deleted passkey does not sign in.
	b := newAgent(t, h.ts)
	if code, body := h.signIn(t, b, 2); code != http.StatusUnauthorized {
		t.Errorf("a deleted passkey signed in: %d: %s", code, body)
	}
}

func TestTheListNeedsAuthentication(t *testing.T) {
	h := newPasskeyHarness(t)
	a := newAgent(t, h.ts)
	if code, _ := a.do("GET", "/v1/passkeys", ""); code != http.StatusUnauthorized {
		t.Error("the list answered without a token")
	}
	if code, _ := a.do("DELETE", "/v1/passkeys/anything", ""); code != http.StatusUnauthorized {
		t.Error("the delete answered without a token")
	}
}

// --- the relying party -------------------------------------------------------

func TestRelyingPartyComesFromConfigurationOrTheRequest(t *testing.T) {
	s := &Server{Now: time.Now}
	r := &http.Request{Host: "teha.example"}
	id, origin, err := s.relyingParty(r)
	if err != nil {
		t.Fatal(err)
	}
	if id != "teha.example" || origin != "https://teha.example" {
		t.Errorf("the request host gave %q and %q", id, origin)
	}

	// A loopback name is the one place a browser accepts plain http.
	r = &http.Request{Host: "127.0.0.1:8637"}
	id, origin, err = s.relyingParty(r)
	if err != nil {
		t.Fatal(err)
	}
	if id != "127.0.0.1" || origin != "http://127.0.0.1:8637" {
		t.Errorf("a loopback host gave %q and %q", id, origin)
	}

	// Configuration wins over the request.
	s.RP = RelyingParty{ID: "teha.example", Origin: "https://teha.example"}
	id, origin, err = s.relyingParty(&http.Request{Host: "evil.example"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "teha.example" || origin != "https://teha.example" {
		t.Errorf("configuration lost to the request: %q and %q", id, origin)
	}

	if _, _, err := s.relyingParty(&http.Request{}); err == nil {
		t.Error("a request with no host produced a relying party")
	}
}

func TestLoopbackNames(t *testing.T) {
	for _, host := range []string{"localhost", "localhost:8637", "127.0.0.1:8637", "[::1]:8637", "app.localhost"} {
		if !isLoopback(host) {
			t.Errorf("%q is not read as loopback", host)
		}
	}
	for _, host := range []string{"teha.example", "teha.example:8637", "203.0.113.7"} {
		if isLoopback(host) {
			t.Errorf("%q is read as loopback", host)
		}
	}
}

// The token path must not change. A bearer token still opens every route, and
// no passkey state is needed for it.
func TestTheDeviceTokenStillWorksWithNoPasskey(t *testing.T) {
	h := newPasskeyHarness(t)
	a := newAgent(t, h.ts)
	a.token = testToken
	for _, path := range []string{"/v1/tasks", "/v1/projects", "/v1/export"} {
		if code, body := a.do("GET", path, ""); code != http.StatusOK {
			t.Errorf("%s with the token returned %d: %s", path, code, body)
		}
	}
}
