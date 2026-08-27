// SPDX-License-Identifier: AGPL-3.0-or-later

// Package api serves the HTTP surface: sync, query, export and the event
// stream. The web app talks to this, and so does every native client.
package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lightheaded/teha/filter"
	"github.com/lightheaded/teha/internal/store"
)

// Server holds the store and the live-update fan-out.
type Server struct {
	Store *store.Store
	Log   *slog.Logger
	// Token guards every route. An empty token turns auth off, which is for
	// local development only.
	Token string
	// Now returns the current time. Tests replace it.
	Now func() time.Time

	mu       sync.Mutex
	watchers map[chan int64]struct{}
}

// New builds a server.
func New(s *store.Store, token string, log *slog.Logger) *Server {
	return &Server{Store: s, Token: token, Log: log, Now: time.Now, watchers: map[chan int64]struct{}{}}
}

// Routes returns the HTTP handler for the API.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sync", s.guard(s.handleSync))
	mux.HandleFunc("GET /v1/tasks", s.guard(s.handleTasks))
	mux.HandleFunc("GET /v1/projects", s.guard(s.handleProjects))
	mux.HandleFunc("GET /v1/sections", s.guard(s.handleSections))
	mux.HandleFunc("GET /v1/labels", s.guard(s.handleLabels))
	mux.HandleFunc("GET /v1/export", s.guard(s.handleExport))
	mux.HandleFunc("GET /v1/events", s.guard(s.handleEvents))
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	return mux
}

// Notify wakes every event stream after a write.
func (s *Server) Notify(version int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.watchers {
		select {
		case ch <- version:
		default: // a slow client pulls on its own schedule
		}
	}
}

func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Token == "" {
			h(w, r)
			return
		}
		if s.authorized(r) {
			h(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="teha"`)
		writeErr(w, http.StatusUnauthorized, "a token is required")
	}
}

func (s *Server) authorized(r *http.Request) bool {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		if constantEqual(strings.TrimPrefix(h, "Bearer "), s.Token) {
			return true
		}
	}
	if c, err := r.Cookie("teha_token"); err == nil && constantEqual(c.Value, s.Token) {
		return true
	}
	return false
}

// constantEqual compares two strings without an early exit on the first
// difference, so the comparison time does not leak the token.
func constantEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// --- sync -------------------------------------------------------------------

type syncRequest struct {
	Since    int64           `json:"since"`
	Commands []store.Command `json:"commands"`
}

type syncResponse struct {
	Version  int64           `json:"version"`
	Applied  []store.Result  `json:"applied"`
	Projects []store.Project `json:"projects"`
	Sections []store.Section `json:"sections"`
	Labels   []store.Label   `json:"labels"`
	Tasks    []store.Task    `json:"tasks"`
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	var req syncRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "cannot read the request: "+err.Error())
		return
	}
	if len(req.Commands) > 200 {
		writeErr(w, http.StatusBadRequest, "at most 200 commands per request")
		return
	}
	// An empty slice, not a nil one. A nil slice marshals to `null`, and a
	// client that declares this field as a list then fails to parse the whole
	// answer. The Android app hit exactly that on its first connection test:
	//   Expected start of the array '[', but had 'n' instead at path: $.applied
	// A list field in this API is always a list, never null.
	results := []store.Result{}
	var err error
	if len(req.Commands) > 0 {
		_, results, err = s.Store.Apply(req.Commands)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	delta, err := s.Store.Pull(req.Since)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(req.Commands) > 0 {
		s.Notify(delta.Version)
	}
	writeJSON(w, http.StatusOK, syncResponse{
		Version: delta.Version, Applied: results,
		Projects: orEmpty(delta.Projects), Sections: orEmpty(delta.Sections),
		Labels: orEmpty(delta.Labels),
		Tasks:  orEmpty(delta.Tasks),
	})
}

// orEmpty turns a nil slice into an empty one, so that a list field never
// marshals to `null`. Go makes that distinction and JSON clients mostly do not:
// a typed client declares `List<T>` and a null answer fails the whole parse.
func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// --- reads ------------------------------------------------------------------

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("filter")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	where, args, err := filter.Compile(q, s.Now())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	tasks, err := s.Store.Query(where, args, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []store.Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "filter": q})
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.Projects()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": ps})
}

func (s *Server) handleSections(w http.ResponseWriter, r *http.Request) {
	secs, err := s.Store.Sections()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sections": secs})
}

func (s *Server) handleLabels(w http.ResponseWriter, r *http.Request) {
	ls, err := s.Store.Labels()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": ls})
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	delta, err := s.Store.Pull(0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="teha-export.json"`)
	writeJSON(w, http.StatusOK, delta)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.Version()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": v})
}

// handleEvents streams a version number on every write, so a client pulls
// only when something changed.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	ch := make(chan int64, 8)
	s.mu.Lock()
	s.watchers[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.watchers, ch)
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case v := <-ch:
			fmt.Fprintf(w, "event: version\ndata: %d\n\n", v)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// --- helpers ----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}
