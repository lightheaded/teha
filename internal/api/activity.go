// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"strconv"

	"github.com/lightheaded/teha/internal/store"
)

// The activity log is read over its own route and never through sync.
//
// Every other table a client holds is state it works from offline. A log is
// not: it only grows, an import writes one line per command, and a person
// opens the view rarely. So the client asks for a page when it needs one, and
// keeps none of it. See internal/store/activity.go.

// handleActivity answers one page of the log, newest first.
//
// project and task narrow it. before is the paging cursor: pass the smallest
// seq of the previous page to get the page under it.
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	me, ok := s.mustCaller(w, r)
	if !ok {
		return
	}
	q := store.ActivityQuery{
		ProjectID: r.URL.Query().Get("project"),
		TaskID:    r.URL.Query().Get("task"),
	}
	q.Before, _ = strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)

	limit := defaultActivityPage
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = n
	}
	if limit > maxActivityPage {
		limit = maxActivityPage
	}
	// One row more than the page. It never reaches the client, and it is the
	// only honest way to say whether another page exists: a count over the
	// whole log costs a scan, and a page that happens to be exactly full
	// otherwise reads as the end.
	q.Limit = limit + 1

	rows, err := s.Store.ActivityFor(q, me.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"activity": rows, "more": more})
}

const (
	// The page a client gets when it names no size. A day of a busy household
	// fits, and the view offers a button for the rest.
	defaultActivityPage = 50
	// The largest page a client may ask for. store.ActivityFor caps it again,
	// so this is the polite limit and not the safe one.
	maxActivityPage = 100
)
