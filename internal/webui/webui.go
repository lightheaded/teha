// SPDX-License-Identifier: AGPL-3.0-or-later

// Package webui holds the built web application and serves it.
package webui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
)

//go:embed all:assets
var files embed.FS

// FS returns the asset tree.
func FS() fs.FS {
	sub, err := fs.Sub(files, "assets")
	if err != nil {
		panic(err) // the embed is built into the binary, so this cannot happen
	}
	return sub
}

// Read returns one asset by name, for example "login.html".
func Read(name string) ([]byte, error) {
	return files.ReadFile("assets/" + name)
}

// etags maps an asset path to the hash of its content. An embedded file has a
// zero modification time, so the standard file server cannot build a validator
// and a browser keeps a stale script after an upgrade. A content hash fixes
// that, and it also lets a client cache hard between releases.
var (
	etagOnce sync.Once
	etags    map[string]string
)

func etagFor(name string) string {
	etagOnce.Do(func() {
		etags = map[string]string{}
		_ = fs.WalkDir(FS(), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			f, err := FS().Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			h := sha256.New()
			if _, err := io.Copy(h, f); err != nil {
				return nil
			}
			etags["/"+p] = `"` + hex.EncodeToString(h.Sum(nil))[:16] + `"`
			return nil
		})
	})
	return etags[name]
}

// Handler serves the assets with a content ETag and the security headers the
// app needs.
func Handler() http.Handler {
	srv := http.FileServer(http.FS(FS()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path
		if name == "/" || strings.HasSuffix(name, "/") {
			name = path.Join(name, "index.html")
		}
		if tag := etagFor(name); tag != "" {
			w.Header().Set("ETag", tag)
			// Revalidate every time: a task app must never run yesterday's
			// script against today's server.
			w.Header().Set("Cache-Control", "no-cache")
			if match := r.Header.Get("If-None-Match"); match == tag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		srv.ServeHTTP(w, r)
	})
}
