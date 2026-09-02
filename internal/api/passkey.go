// SPDX-License-Identifier: AGPL-3.0-or-later

// Passkeys: WebAuthn registration and login for the one owner account.
//
// The device token stays. A passkey is a second way into the same account, for
// the browser, and the token is what an Android client, the command line client
// and MCP keep using. Nothing here can turn the token path off.
//
// # The user-verification policy
//
// Every ceremony asks for user verification, and the server rejects an
// assertion that reports none: Config.AuthenticatorSelection.UserVerification
// is protocol.VerificationRequired, so the library checks the UV flag on both
// the registration and the login. A passkey is the whole login on a public
// hostname, and a stolen unlocked phone must not be an account. The cost is
// that a security key without a PIN cannot enrol. See DECISIONS.md D-009.
//
// # Fail closed
//
// - The relying-party id and the origin of a ceremony are fixed when the
//   ceremony begins, and the finish step reads them from the stored ceremony
//   and never from the request. A caller that changes the Host header between
//   the two steps therefore fails.
// - A ceremony is single use. The finish step deletes it before it verifies.
// - A signature counter that does not increase is a refusal, not a warning.
// - An unknown credential id, an unknown user handle and a bad signature all
//   answer 401 with the same words.
// - Repeated failures lock out the client address and the account.

package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/lightheaded/teha/internal/store"
)

const (
	// sessionCookieName holds the passkey session. It is separate from
	// teha_token, so a passkey login never learns the device token and a
	// revoked session never touches the token.
	sessionCookieName = "teha_session"
	// ceremonyCookieName holds the handle of one in-flight WebAuthn ceremony.
	// The challenge itself stays on the server, because a user agent that can
	// read or write the challenge can replay it.
	ceremonyCookieName = "teha_ceremony"

	// DefaultSessionTTL is how long a passkey session lasts. Fourteen days is
	// the shape of a daily driver: the owner opens the app every day and signs
	// in again about twice a month.
	DefaultSessionTTL = 14 * 24 * time.Hour
	// ceremonyTTL bounds one registration or login. A browser prompt that
	// nobody answers must not stay valid.
	ceremonyTTL = 5 * time.Minute

	// A login may fail this many times per client address before the lockout
	// starts. A person mistakes the wrong passkey once or twice.
	ipFailureAllowance = 5
	// The account has its own budget, so a botnet with one address per attempt
	// still runs into a wall.
	accountFailureAllowance = 10
	lockoutBase             = 30 * time.Second
	lockoutMax              = 15 * time.Minute
)

// RelyingParty names this deployment to an authenticator. An empty field is
// read from the request, so no hostname is built into the binary.
type RelyingParty struct {
	// ID is the relying-party id, a bare domain such as teha.example. A
	// credential is bound to it for life, so it must not change.
	ID string
	// Origin is the full origin, such as https://teha.example.
	Origin string
	// DisplayName is what the authenticator prompt shows.
	DisplayName string
}

// webSession is one signed-in browser.
// ceremony is one in-flight WebAuthn exchange.
type ceremony struct {
	hash    string
	kind    string // "register" or "login"
	session webauthn.SessionData
	rpID    string
	origin  string
	expires time.Time
}

// failCount is the lockout state of one client address, or of the account.
type failCount struct {
	n int
	// until is when the lockout ends. A zero value means no lockout yet.
	until time.Time
	// last is the time of the newest failure. The sweep reads it, so an
	// address that stops trying leaves the map instead of holding a row for
	// ever. Without it an attacker grows this map one row per address.
	last time.Time
}

// passkeyRoutes mounts the passkey surface.
//
// Registration sits behind the device token and nothing else. The token is the
// one invitation into this account, which keeps signup invite-only and adds no
// new concept. A passkey session can list and delete credentials, but it
// cannot enrol a new one.
func (s *Server) passkeyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/passkeys/register/begin", s.guardToken(s.handleRegisterBegin))
	mux.HandleFunc("POST /v1/passkeys/register/finish", s.guardToken(s.handleRegisterFinish))
	mux.HandleFunc("POST /v1/passkeys/login/begin", s.handleLoginBegin)
	mux.HandleFunc("POST /v1/passkeys/login/finish", s.handleLoginFinish)
	mux.HandleFunc("GET /v1/passkeys", s.guard(s.handlePasskeyList))
	mux.HandleFunc("DELETE /v1/passkeys/{id}", s.guard(s.handlePasskeyDelete))
	mux.HandleFunc("POST /v1/logout", s.handleLogout)
}

// --- the relying party ------------------------------------------------------

// relyingParty resolves the identity of this deployment for one request.
// Configuration wins. Without it the values come from the request host, so a
// self-hoster needs no hostname in a file and no hostname is in the binary.
func (s *Server) relyingParty(r *http.Request) (rpID, origin string, err error) {
	host := r.Host
	if host == "" {
		return "", "", errors.New("the request carries no host, so the relying party is unknown")
	}
	rpID = s.RP.ID
	if rpID == "" {
		rpID = hostOnly(host)
	}
	origin = s.RP.Origin
	if origin == "" {
		origin = originScheme(host) + "://" + host
	}
	return rpID, origin, nil
}

// hostOnly drops the port. A relying-party id is a bare domain.
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// originScheme states the scheme of the app's own origin.
//
// WebAuthn runs in a secure context only: https anywhere, or http on a
// loopback name. The scheme therefore follows from the host and needs no
// forwarded header, which is what keeps a client from claiming its own scheme.
// A deployment that serves plain http on a real name is not a deployment a
// browser will run a passkey on, so https is the honest default.
func originScheme(host string) string {
	if isLoopback(host) {
		return "http"
	}
	return "https"
}

func isLoopback(host string) bool {
	h := strings.ToLower(hostOnly(host))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// newWebauthn builds the library handle for one ceremony.
//
// The handle is per request on purpose. The relying-party id and the origin can
// come from the request, and the library caches its validated configuration, so
// one shared handle would freeze the first host it ever saw.
func (s *Server) newWebauthn(rpID, origin string) (*webauthn.WebAuthn, error) {
	name := s.RP.DisplayName
	if name == "" {
		name = "teha"
	}
	requireResidentKey := true
	return webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: name,
		RPOrigins:     []string{origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			// A discoverable credential, so the login needs no user name: the
			// authenticator names the account. It also keeps the credential
			// ids off the wire for a caller that has not signed in.
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: &requireResidentKey,
			// The policy of this build. See the package comment and D-009.
			UserVerification: protocol.VerificationRequired,
		},
		// No attestation. This build trusts any authenticator the owner holds,
		// and it runs no metadata service to judge one.
		AttestationPreference: protocol.PreferNoAttestation,
	})
}

// --- the owner --------------------------------------------------------------

// passkeyUser adapts the account and its credentials to the library.
type passkeyUser struct {
	account store.Account
	creds   []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return u.account.UserHandle }
func (u passkeyUser) WebAuthnName() string                       { return u.account.Name }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.account.DisplayName }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (s *Server) ownerUser() (passkeyUser, error) {
	owner, err := s.Store.Owner()
	if err != nil {
		return passkeyUser{}, err
	}
	return s.userFor(owner)
}

func (s *Server) userFor(a store.Account) (passkeyUser, error) {
	rows, err := s.Store.Credentials(a.ID)
	if err != nil {
		return passkeyUser{}, err
	}
	u := passkeyUser{account: a}
	for _, row := range rows {
		u.creds = append(u.creds, toLibraryCredential(row))
	}
	return u, nil
}

// toLibraryCredential rebuilds the credential record the library verifies
// against. Every field it reads on a login comes out of the row: the public
// key, the signature counter and the flag byte of the first ceremony.
func toLibraryCredential(c store.Credential) webauthn.Credential {
	out := webauthn.Credential{
		ID:                decodeCredentialID(c.ID),
		PublicKey:         c.PublicKey,
		AttestationType:   c.AttestationType,
		AttestationFormat: c.AttestationFormat,
		Flags:             webauthn.CredentialFlagsFromMsgpByte(byte(c.Flags)),
	}
	out.Authenticator.SignCount = uint32(c.SignCount)
	if id, err := uuid.Parse(c.AAGUID); err == nil {
		out.Authenticator.AAGUID = id[:]
	}
	for _, t := range strings.Split(c.Transports, ",") {
		if t != "" {
			out.Transport = append(out.Transport, protocol.AuthenticatorTransport(t))
		}
	}
	return out
}

// fromLibraryCredential turns a fresh registration into a row.
func fromLibraryCredential(c *webauthn.Credential, accountID, name, at string) store.Credential {
	transports := make([]string, 0, len(c.Transport))
	for _, t := range c.Transport {
		transports = append(transports, string(t))
	}
	aaguid := ""
	if id, err := uuid.FromBytes(c.Authenticator.AAGUID); err == nil && id != uuid.Nil {
		aaguid = id.String()
	}
	return store.Credential{
		ID:                encodeCredentialID(c.ID),
		AccountID:         accountID,
		PublicKey:         c.PublicKey,
		SignCount:         int64(c.Authenticator.SignCount),
		Transports:        strings.Join(transports, ","),
		AAGUID:            aaguid,
		Name:              name,
		Flags:             int64(c.Flags.MsgpByte()),
		AttestationType:   c.AttestationType,
		AttestationFormat: c.AttestationFormat,
		CreatedAt:         at,
	}
}

// --- registration -----------------------------------------------------------

func (s *Server) handleRegisterBegin(w http.ResponseWriter, r *http.Request) {
	rpID, origin, err := s.relyingParty(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	wa, err := s.newWebauthn(rpID, origin)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "the relying party is not usable: "+err.Error())
		return
	}
	user, err := s.ownerUser()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	creation, session, err := wa.BeginRegistration(user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		// A key already enrolled must not enrol twice. A second row would
		// carry a fresh signature counter for the same authenticator.
		webauthn.WithExclusions(webauthn.Credentials(user.creds).CredentialDescriptors()),
		// Ask for the credential properties. A browser returns this output for
		// a discoverable credential, and the library refuses an output nobody
		// asked for, so asking keeps the strict policy and a working enrolment.
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
	)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot begin the registration: "+err.Error())
		return
	}
	s.startCeremony(w, r, "register", *session, rpID, origin)
	writeJSON(w, http.StatusOK, creation)
}

func (s *Server) handleRegisterFinish(w http.ResponseWriter, r *http.Request) {
	cer, ok := s.takeCeremony(r, "register")
	clearCookie(w, r, ceremonyCookieName)
	if !ok {
		writeErr(w, http.StatusBadRequest, "no registration is in progress, or it expired")
		return
	}
	wa, err := s.newWebauthn(cer.rpID, cer.origin)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "the relying party is not usable: "+err.Error())
		return
	}
	user, err := s.ownerUser()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	cred, err := wa.FinishRegistration(user, cer.session, r)
	if err != nil {
		s.Log.Warn("a passkey registration failed", "err", err)
		writeErr(w, http.StatusBadRequest, "that passkey was not accepted")
		return
	}
	// A fresh credential cannot have a used counter. If the library says the
	// authenticator may be a clone, refuse rather than write the row.
	if cred.Authenticator.CloneWarning {
		writeErr(w, http.StatusBadRequest, "that passkey was not accepted")
		return
	}
	row := fromLibraryCredential(cred, user.account.ID, passkeyName(r), s.Now().UTC().Format(time.RFC3339))
	if err := s.Store.AddCredential(row); err != nil {
		writeErr(w, http.StatusConflict, "that passkey is enrolled already")
		return
	}
	s.Log.Info("a passkey was enrolled", "name", row.Name, "aaguid", row.AAGUID)
	writeJSON(w, http.StatusOK, map[string]any{"passkey": row})
}

// passkeyName reads the friendly name the owner typed. An empty name still
// makes a usable row, because a list of "Passkey" beats a failed enrolment.
func passkeyName(r *http.Request) string {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		return "Passkey"
	}
	if len(name) > 60 {
		name = name[:60]
	}
	return name
}

// --- login ------------------------------------------------------------------

func (s *Server) handleLoginBegin(w http.ResponseWriter, r *http.Request) {
	if wait := s.lockout(r); wait > 0 {
		s.tooManyFailures(w, wait)
		return
	}
	rpID, origin, err := s.relyingParty(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	wa, err := s.newWebauthn(rpID, origin)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "the relying party is not usable: "+err.Error())
		return
	}
	// A discoverable login sends no credential list, so this answer tells an
	// unauthenticated caller nothing about which passkeys exist.
	assertion, session, err := wa.BeginDiscoverableLogin()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot begin the login: "+err.Error())
		return
	}
	s.startCeremony(w, r, "login", *session, rpID, origin)
	writeJSON(w, http.StatusOK, assertion)
}

func (s *Server) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	if wait := s.lockout(r); wait > 0 {
		s.tooManyFailures(w, wait)
		return
	}
	cer, ok := s.takeCeremony(r, "login")
	clearCookie(w, r, ceremonyCookieName)
	if !ok {
		writeErr(w, http.StatusBadRequest, "no login is in progress, or it expired")
		return
	}
	wa, err := s.newWebauthn(cer.rpID, cer.origin)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "the relying party is not usable: "+err.Error())
		return
	}

	// The handler names the account the authenticator claims. An unknown user
	// handle is a refusal: it must never fall back to the owner.
	var owner passkeyUser
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		account, err := s.Store.AccountByUserHandle(userHandle)
		if err != nil {
			return nil, errors.New("no account carries that user handle")
		}
		owner, err = s.userFor(account)
		if err != nil {
			return nil, err
		}
		if _, err := s.Store.Credential(encodeCredentialID(rawID)); err != nil {
			return nil, errors.New("no passkey carries that credential id")
		}
		return owner, nil
	}

	_, cred, err := wa.FinishPasskeyLogin(handler, cer.session, r)
	if err != nil {
		s.failedLogin(r, "the assertion did not verify", err)
		writeErr(w, http.StatusUnauthorized, "that passkey did not sign in")
		return
	}
	// Step 17 of the specification. The library records a clone warning when
	// the counter does not increase. This build refuses, because a replayed
	// assertion and a cloned authenticator both fail this test and both mean
	// the account is under attack.
	if cred.Authenticator.CloneWarning {
		s.failedLogin(r, "the signature counter did not increase", nil)
		writeErr(w, http.StatusUnauthorized, "that passkey did not sign in")
		return
	}

	now := s.Now().UTC().Format(time.RFC3339)
	if err := s.Store.TouchCredential(encodeCredentialID(cred.ID), int64(cred.Authenticator.SignCount),
		int64(cred.Flags.MsgpByte()), now); err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot record the login: "+err.Error())
		return
	}
	s.clearFailures(r)
	s.issueSession(w, r)
	s.Log.Info("a passkey signed in", "credential", encodeCredentialID(cred.ID))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// failedLogin records one refusal and says why in the log. The answer to the
// caller stays the same words for every cause.
func (s *Server) failedLogin(r *http.Request, reason string, err error) {
	s.recordFailure(r)
	s.Log.Warn("a passkey login failed", "reason", reason, "err", err, "ip", s.clientIP(r))
}

func (s *Server) tooManyFailures(w http.ResponseWriter, wait time.Duration) {
	secs := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeErr(w, http.StatusTooManyRequests, "too many failed sign-ins. Wait and try again")
}

// --- list and delete --------------------------------------------------------

func (s *Server) handlePasskeyList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.Credentials(store.OwnerID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": rows})
}

func (s *Server) handlePasskeyDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.DeleteCredential(id); err != nil {
		writeErr(w, http.StatusNotFound, "no passkey carries that id")
		return
	}
	s.Log.Info("a passkey was removed", "credential", id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- the session ------------------------------------------------------------

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if err := s.Store.DeleteSession(c.Value); err != nil {
			s.Log.Error("cannot close the session", "err", err)
		}
	}
	clearCookie(w, r, sessionCookieName)
	clearCookie(w, r, ceremonyCookieName)
	// The browser forgets the device token as well. Logout means the screen in
	// front of the user, so leaving the token behind would sign nobody out.
	clearCookie(w, r, "teha_token")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// sessionTTL states the lifetime of a passkey session.
func (s *Server) sessionTTL() time.Duration {
	if s.SessionTTL > 0 {
		return s.SessionTTL
	}
	return DefaultSessionTTL
}

// issueSession opens a session for the owner. A passkey login that knows which
// account it signed in calls issueSessionFor instead.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request) {
	s.issueSessionFor(w, r, store.OwnerID)
}

// issueSessionFor opens a session for one account and sets the cookie.
//
// The session lives in the database, not in memory. Two reasons, and the
// second one is the reason it moved: a restart no longer signs every browser
// out, and a session has to name the account it belongs to once the file holds
// more than one person.
func (s *Server) issueSessionFor(w http.ResponseWriter, r *http.Request, accountID string) {
	value, err := s.Store.NewSession(accountID, s.sessionTTL())
	if err != nil {
		s.Log.Error("cannot open a session", "err", err)
		return
	}
	setCookie(w, r, sessionCookieName, value, s.Now().Add(s.sessionTTL()), s.sessionTTL())
}

// sessionValid reports whether the request carries a live session.
func (s *Server) sessionValid(r *http.Request) bool {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	_, err = s.Store.SessionAccount(c.Value)
	return err == nil
}

// --- the ceremony store -----------------------------------------------------

func (s *Server) startCeremony(w http.ResponseWriter, r *http.Request, kind string,
	session webauthn.SessionData, rpID, origin string) {
	// The library compares its own Expires against the wall clock, and this
	// server drives time through Now. Keep one clock: drop the library value
	// and enforce the deadline here.
	session.Expires = time.Time{}
	value := randomToken()
	key := cookieKey(value)
	expires := s.Now().Add(ceremonyTTL)
	s.sessMu.Lock()
	if s.ceremonies == nil {
		s.ceremonies = map[string]ceremony{}
	}
	s.sweepLocked()
	s.ceremonies[key] = ceremony{
		hash: key, kind: kind, session: session,
		rpID: rpID, origin: origin, expires: expires,
	}
	s.sessMu.Unlock()
	setCookie(w, r, ceremonyCookieName, value, expires, ceremonyTTL)
}

// takeCeremony reads one ceremony and removes it. A ceremony is single use, so
// a replayed finish request finds nothing.
func (s *Server) takeCeremony(r *http.Request, kind string) (ceremony, bool) {
	c, err := r.Cookie(ceremonyCookieName)
	if err != nil || c.Value == "" {
		return ceremony{}, false
	}
	key := cookieKey(c.Value)
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	cer, ok := s.ceremonies[key]
	if !ok {
		return ceremony{}, false
	}
	delete(s.ceremonies, key)
	if cer.kind != kind || !constantEqual(cer.hash, key) {
		return ceremony{}, false
	}
	if !s.Now().Before(cer.expires) {
		return ceremony{}, false
	}
	return cer, true
}

// sweepLocked drops expired rows. The caller holds sessMu.
func (s *Server) sweepLocked() {
	now := s.Now()
	for k, v := range s.ceremonies {
		if !now.Before(v.expires) {
			delete(s.ceremonies, k)
		}
	}
	// A session lives in the database now, and it prunes itself there.
	if err := s.Store.PruneSessions(); err != nil {
		s.Log.Error("cannot prune the sessions", "err", err)
	}
	// A quiet period forgets a failure. Two reasons, and both matter:
	// an old attack must not leave the owner at the longest wait for ever, and
	// an unauthenticated caller must not grow this map one row per address.
	for k, v := range s.failures {
		if v.n == 0 || stale(*v, now) {
			delete(s.failures, k)
		}
	}
	if s.accountFails.n > 0 && stale(s.accountFails, now) {
		s.accountFails = failCount{}
	}
}

// stale reports whether a failure count has aged out: the lockout is over, and
// the newest failure is older than the longest lockout.
func stale(f failCount, now time.Time) bool {
	if f.until.After(now) {
		return false
	}
	return !f.last.IsZero() && now.Sub(f.last) > lockoutMax
}

// --- the lockout ------------------------------------------------------------

// clientIP names the caller for the lockout.
//
// A forwarded header is read only when the operator says a proxy writes it. A
// client that can set its own address escapes every ban, so the default is the
// address of the connection.
func (s *Server) clientIP(r *http.Request) string {
	if s.TrustForwarded {
		if h := r.Header.Get("X-Forwarded-For"); h != "" {
			parts := strings.Split(h, ",")
			// The proxy appends its own view of the client, so the last entry
			// is the one entry no client could write.
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// lockout reports how long this caller must wait. Zero means it may proceed.
func (s *Server) lockout(r *http.Request) time.Duration {
	ip := s.clientIP(r)
	now := s.Now()
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	s.sweepLocked()
	wait := time.Duration(0)
	if f, ok := s.failures[ip]; ok && f.until.After(now) {
		wait = f.until.Sub(now)
	}
	if s.accountFails.until.After(now) {
		if d := s.accountFails.until.Sub(now); d > wait {
			wait = d
		}
	}
	return wait
}

// recordFailure counts one refused login and extends the lockout.
func (s *Server) recordFailure(r *http.Request) {
	ip := s.clientIP(r)
	now := s.Now()
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	if s.failures == nil {
		s.failures = map[string]*failCount{}
	}
	f, ok := s.failures[ip]
	if !ok {
		f = &failCount{}
		s.failures[ip] = f
	}
	f.n++
	f.last = now
	if d := lockDuration(f.n, ipFailureAllowance); d > 0 {
		f.until = now.Add(d)
	}
	s.accountFails.n++
	s.accountFails.last = now
	if d := lockDuration(s.accountFails.n, accountFailureAllowance); d > 0 {
		s.accountFails.until = now.Add(d)
	}
}

// clearFailures forgets the counters after a login that worked.
func (s *Server) clearFailures(r *http.Request) {
	ip := s.clientIP(r)
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	delete(s.failures, ip)
	s.accountFails = failCount{}
}

// lockDuration gives the wait after n failures. The wait doubles with every
// failure above the allowance, up to a cap.
func lockDuration(n, allowance int) time.Duration {
	if n <= allowance {
		return 0
	}
	steps := n - allowance - 1
	if steps > 20 {
		return lockoutMax
	}
	d := lockoutBase << steps
	if d > lockoutMax {
		return lockoutMax
	}
	return d
}

// --- cookies and ids --------------------------------------------------------

// setCookie writes one of this package's cookies.
//
// Secure follows the host and not r.TLS. The server usually sits behind a
// proxy that ends TLS, so r.TLS is nil on a request that reached the browser
// over https. Reading the host instead makes the flag right in both places,
// and it leaves plain http working on a loopback name for development.
func setCookie(w http.ResponseWriter, r *http.Request, name, value string, expires time.Time, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !isLoopback(r.Host),
		Expires:  expires,
		MaxAge:   int(ttl.Seconds()),
	})
}

func clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !isLoopback(r.Host),
		MaxAge:   -1,
	})
}

// SecureCookie reports whether a cookie for this request must carry Secure.
// The login handler in the command asks, so the device token cookie and the
// passkey session cookie follow one rule.
func SecureCookie(r *http.Request) bool {
	return !isLoopback(r.Host)
}

// cookieKey hashes a cookie value. The server stores the hash, so a dump of
// its memory or its logs holds no usable cookie.
func cookieKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // a machine without a random source cannot serve auth
	}
	return hex.EncodeToString(b)
}

// encodeCredentialID names a credential the way a browser does: base64url with
// no padding. One spelling on the wire, in the table and in a log line.
func encodeCredentialID(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCredentialID reverses encodeCredentialID. A row that cannot decode
// yields no bytes, so it matches no assertion and the login fails closed.
func decodeCredentialID(id string) []byte {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return nil
	}
	return raw
}
