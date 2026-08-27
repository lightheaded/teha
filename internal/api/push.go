// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lightheaded/teha/internal/store"
)

// Pusher is the Web Push sender. The api package needs two things from it, so
// it asks for two things: the key a browser subscribes with, and a test
// message. internal/push implements it.
type Pusher interface {
	PublicKey() string
	SendTest(ctx context.Context) (int, error)
}

// subscribeRequest is the shape a browser produces from
// PushSubscription.toJSON(), so the web app posts it without translation.
type subscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// handlePushKey tells the browser whether push is on, and with which key.
// A server with no VAPID private key answers enabled:false, and the settings
// area then says so instead of failing at the subscribe call.
func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.CountSubscriptions()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	key := ""
	if s.Push != nil {
		key = s.Push.PublicKey()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": key != "", "key": key, "devices": n,
	})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "cannot read the subscription: "+err.Error())
		return
	}
	err := s.Store.SaveSubscription(store.PushSubscription{
		Endpoint:  req.Endpoint,
		P256dh:    req.Keys.P256dh,
		Auth:      req.Keys.Auth,
		UserAgent: shortUA(r.UserAgent()),
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "cannot read the subscription: "+err.Error())
		return
	}
	if req.Endpoint == "" {
		writeErr(w, http.StatusBadRequest, "an endpoint is required")
		return
	}
	if err := s.Store.DeleteSubscription(req.Endpoint); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handlePushTest sends one notification to every subscribed device. A person
// who turns notifications on wants to see one, now, not at the next due time.
func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	if s.Push == nil {
		writeErr(w, http.StatusServiceUnavailable, "this server has no VAPID key, so it cannot send push")
		return
	}
	n, err := s.Push.SendTest(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": n})
}

// shortUA keeps a readable label and drops the rest. The full string is a
// fingerprint and it is of no use here: the label only tells a person which of
// their devices a row is.
func shortUA(ua string) string {
	platform := "a desktop"
	switch {
	case strings.Contains(ua, "Android"):
		platform = "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		platform = "iOS"
	case strings.Contains(ua, "Macintosh"):
		platform = "macOS"
	case strings.Contains(ua, "Windows"):
		platform = "Windows"
	case strings.Contains(ua, "Linux"):
		platform = "Linux"
	}
	// The order matters: every browser claims Safari, and Edge claims Chrome.
	for _, name := range []string{"Firefox", "Edg", "Chrome", "Safari"} {
		if strings.Contains(ua, name) {
			if name == "Edg" {
				name = "Edge"
			}
			return name + " on " + platform
		}
	}
	if len(ua) > 40 {
		return ua[:40]
	}
	return ua
}
