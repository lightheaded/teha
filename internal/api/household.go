// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lightheaded/teha/internal/store"
)

// The household over HTTP: who is asking, how a second person gets in, and how
// a list is shared.
//
// Every guarded route answers for one account. caller resolves it once, from a
// device token or from a session cookie, and every handler works from what it
// returns. A route that forgot to ask would answer for the owner, so the guard
// puts the account on the request and the handlers read it from there.

// hCallerKey is the request context key that carries the account.
type ctxKey int

const callerKey ctxKey = 1

// caller returns the account behind a request. The second value is false when
// the request carries nothing that names one.
//
// The order is deliberate. A device token names one account exactly, and it is
// what every native client, the command line client and MCP send. A session
// cookie is the browser after a passkey login. A dev server with no token
// answers as the owner, which is what a laptop with no secret wants.
func (s *Server) caller(r *http.Request) (store.Account, bool) {
	if a, ok := r.Context().Value(callerKey).(store.Account); ok {
		return a, true
	}
	if a, ok := s.tokenCaller(r); ok {
		return a, true
	}
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		if a, err := s.Store.SessionAccount(c.Value); err == nil {
			return a, true
		}
	}
	if s.Token == "" {
		// A development server with no token. Everything is the owner's.
		if a, err := s.Store.Owner(); err == nil {
			return a, true
		}
	}
	return store.Account{}, false
}

// tokenCaller resolves a device token to its account.
func (s *Server) tokenCaller(r *http.Request) (store.Account, bool) {
	token := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimPrefix(h, "Bearer ")
	} else if c, err := r.Cookie("teha_token"); err == nil {
		token = c.Value
	}
	if token == "" {
		return store.Account{}, false
	}
	if a, err := s.Store.AccountForToken(token); err == nil {
		return a, true
	}
	// The token in the configuration is the owner's. It is written into the
	// account row at start-up as well, and this is the path that still works
	// when it was not: a read-only file, or a token changed since.
	if s.Token != "" && constantEqual(token, s.Token) {
		if a, err := s.Store.Owner(); err == nil {
			return a, true
		}
	}
	return store.Account{}, false
}

// mustCaller answers the account or writes the refusal. A handler behind guard
// always has one, so the false path is the belt.
func (s *Server) mustCaller(w http.ResponseWriter, r *http.Request) (store.Account, bool) {
	a, ok := s.caller(r)
	if !ok {
		s.denied(w)
		return a, false
	}
	return a, true
}

// Caller is caller, for another package. The MCP server needs to know which
// account a bearer token belongs to, and it must not read a cookie name or a
// token rule of its own.
func (s *Server) Caller(r *http.Request) (store.Account, bool) { return s.caller(r) }

// WithCaller puts an account on a request, so a handler further down reads it
// with Caller instead of resolving it a second time.
func WithCaller(r *http.Request, a store.Account) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), callerKey, a))
}

// GuardHandler wraps a whole handler in the same rule as guard. The MCP
// endpoint uses it, so an invited person can drive an agent against their own
// account with their own token.
func (s *Server) GuardHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := s.caller(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="teha"`)
			s.denied(w)
			return
		}
		next.ServeHTTP(w, WithCaller(r, a))
	})
}

// ownerOnly refuses everybody but the owner. Inviting a person into the house
// is the owner's to do, and nobody else's.
func (s *Server) ownerOnly(h http.HandlerFunc) http.HandlerFunc {
	return s.guard(func(w http.ResponseWriter, r *http.Request) {
		a, ok := s.mustCaller(w, r)
		if !ok {
			return
		}
		if !a.IsOwner && a.ID != store.OwnerID {
			writeErr(w, http.StatusForbidden, "only the owner of this server can do that")
			return
		}
		h(w, r)
	})
}

// householdRoutes adds the routes that a second person needs.
func (s *Server) householdRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/household", s.guard(s.handleHousehold))
	mux.HandleFunc("POST /v1/invites", s.ownerOnly(s.handleInviteCreate))
	mux.HandleFunc("GET /v1/invites", s.ownerOnly(s.handleInviteList))
	mux.HandleFunc("POST /v1/invites/revoke", s.ownerOnly(s.handleInviteRevoke))
	// Joining is the one route with no guard: the code is the credential.
	mux.HandleFunc("POST /v1/join", s.handleJoin)
	mux.HandleFunc("POST /v1/share", s.guard(s.handleShare))
}

// handleHousehold answers who is in the house, and which lists are shared.
// Every client draws its sharing panel from this.
func (s *Server) handleHousehold(w http.ResponseWriter, r *http.Request) {
	me, ok := s.mustCaller(w, r)
	if !ok {
		return
	}
	people, err := s.Store.Accounts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	shares, err := s.Store.Shares()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A member sees who else is in the house, because a shared list shows
	// their name on a task. Nothing else about them travels.
	type person struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		IsOwner bool   `json:"is_owner"`
		IsMe    bool   `json:"is_me"`
	}
	out := []person{}
	for _, a := range people {
		out = append(out, person{ID: a.ID, Name: a.DisplayName, IsOwner: a.IsOwner, IsMe: a.ID == me.ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"me":     me.ID,
		"inbox":  me.InboxID,
		"people": out,
		"shares": shares,
	})
}

func (s *Server) handleInviteCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "cannot read the request: "+err.Error())
		return
	}
	me, ok := s.mustCaller(w, r)
	if !ok {
		return
	}
	inv, err := s.Store.CreateInvite(me.ID, req.Name, store.InviteTTL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Store.Note(me.ID, store.ActionInviteCreate, inv.Name, "", s.clientIP(r))
	s.Log.Info("an invitation was written", "for", inv.Name, "expires", inv.ExpiresAt)
	// The code is in this answer and in no other. The store keeps its hash.
	writeJSON(w, http.StatusOK, inv)
}

func (s *Server) handleInviteList(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Invites()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": list})
}

func (s *Server) handleInviteRevoke(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "cannot read the request: "+err.Error())
		return
	}
	if err := s.Store.RevokeInvite(req.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if me, ok := s.caller(r); ok {
		s.Store.Note(me.ID, store.ActionInviteRevoke, "", "", s.clientIP(r))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleJoin turns an invitation code into an account, and signs the browser
// in. It is the only route that answers without a credential, so it counts a
// failure the same way a passkey login does.
func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "cannot read the request: "+err.Error())
		return
	}
	if wait := s.lockout(r); wait > 0 {
		s.tooManyFailures(w, wait)
		return
	}

	account, token, err := s.Store.RedeemInvite(strings.TrimSpace(req.Code), req.Name)
	if err != nil {
		if errors.Is(err, store.ErrBadInvite) {
			s.recordFailure(r)
			s.Log.Warn("an invitation code was refused", "from", s.clientIP(r))
			writeErr(w, http.StatusUnauthorized, "that invitation is not valid")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.clearFailures(r)
	// Two lines, because two people need this one. The owner reads it in their
	// own log as the invitation being used, and the new account reads its own
	// first line.
	s.Store.Note(store.OwnerID, store.ActionJoined, account.DisplayName, "", s.clientIP(r))
	s.Store.Note(account.ID, store.ActionJoined, account.DisplayName, "", s.clientIP(r))
	s.Log.Info("somebody joined the household", "account", account.ID, "name", account.DisplayName)

	// The browser keeps the device token in a cookie, exactly as the owner's
	// browser does, and it also gets a session so that the first screen is
	// already signed in.
	setCookie(w, r, "teha_token", token, s.Now().Add(365*24*time.Hour), 365*24*time.Hour)
	s.issueSessionFor(w, r, account.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "account": account.ID, "name": account.DisplayName,
		// The token is in this answer once, so a person can put it into the
		// phone app and into an MCP client.
		"token": token,
	})
}

// handleShare shares one list with one person, or takes it back.
//
// Only the owner of that list may do either. A member works inside a shared
// list and does not pass it on.
func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID string `json:"project_id"`
		AccountID string `json:"account_id"`
		Share     bool   `json:"share"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "cannot read the request: "+err.Error())
		return
	}
	me, ok := s.mustCaller(w, r)
	if !ok {
		return
	}
	owner, err := s.Store.ProjectOwner(req.ProjectID)
	if err != nil {
		// The same answer as a list that belongs to somebody else, so a caller
		// cannot ask which lists the household holds.
		writeErr(w, http.StatusForbidden, "that list is not yours to share")
		return
	}
	if owner != me.ID {
		writeErr(w, http.StatusForbidden, "that list is not yours to share")
		return
	}
	if req.AccountID == me.ID {
		writeErr(w, http.StatusBadRequest, "a list is already yours")
		return
	}
	if _, err := s.Store.AccountByID(req.AccountID); err != nil {
		writeErr(w, http.StatusBadRequest, "there is nobody by that name in this household")
		return
	}
	if req.Share {
		err = s.Store.ShareProject(req.ProjectID, req.AccountID)
	} else {
		err = s.Store.UnshareProject(req.ProjectID, req.AccountID)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The line belongs to the list, so everybody who can see the list reads it.
	name := ""
	if a, err := s.Store.AccountByID(req.AccountID); err == nil {
		name = a.DisplayName
	}
	if req.Share {
		s.Store.NoteIn(me.ID, store.ActionShare, req.ProjectID, name, "")
	} else {
		// An unshare needs two lines. The one on the list stays for the people
		// who still see it, and the person who lost the list cannot read that
		// line any more, so they get a personal one that says the list went.
		s.Store.NoteIn(me.ID, store.ActionUnshare, req.ProjectID, name, "")
		s.Store.Note(req.AccountID, store.ActionUnshare, projectName(s, req.ProjectID), "", "")
	}
	v, _ := s.Store.Version()
	s.Notify(v)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// projectName reads the name of a list for a log line. An unreadable row gives
// an empty string, and the line then says only that a list went.
func projectName(s *Server, projectID string) string {
	ps, err := s.Store.Projects()
	if err != nil {
		return ""
	}
	for _, p := range ps {
		if p.ID == projectID {
			return p.Name
		}
	}
	return ""
}
