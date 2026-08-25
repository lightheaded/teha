// SPDX-License-Identifier: AGPL-3.0-or-later

// Command teha runs the whole product: the API, the sync endpoint, the web app
// and the MCP server, from one binary and one SQLite file.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lightheaded/teha/internal/api"
	"github.com/lightheaded/teha/internal/mcpsrv"
	"github.com/lightheaded/teha/internal/store"
	"github.com/lightheaded/teha/internal/webui"
)

func main() {
	// Subcommands come before the flag set, so "teha add ..." never collides
	// with a server flag.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "import":
			os.Exit(runImport(os.Args[2:]))
		case "add", "ls", "done", "today", "projects":
			os.Exit(runClient(os.Args[1:]))
		case "serve":
			os.Args = append(os.Args[:1], os.Args[2:]...)
		}
	}

	var (
		addr    = flag.String("addr", envOr("TEHA_ADDR", "127.0.0.1:8637"), "address to listen on")
		dbPath  = flag.String("db", envOr("TEHA_DB", "teha.db"), "path to the SQLite file")
		token   = flag.String("token", os.Getenv("TEHA_TOKEN"), "device token. An empty token in --dev mode turns auth off")
		dev     = flag.Bool("dev", false, "development mode: no auth, verbose logs")
		mcpOn   = flag.Bool("mcp", envBool("TEHA_MCP"), "serve the MCP endpoint at /mcp. Off by default: an agent endpoint drives the account, not only reads it, so an operator turns it on deliberately")
		seed    = flag.Bool("seed", false, "write example data into an empty database and exit")
		seedDay = flag.String("seed-date", "", "the day that seeded dates count from, as 2006-01-02. Empty means today. Used by the screenshot job, so that an unchanged screen produces an identical image on any day")
		version = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()

	if *version {
		fmt.Println("teha 0.1.0 (proof of concept)")
		return
	}

	level := slog.LevelInfo
	if *dev {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if err := os.MkdirAll(filepath.Dir(absDir(*dbPath)), 0o750); err != nil {
		log.Error("cannot create the data directory", "err", err)
		os.Exit(1)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		log.Error("cannot open the database", "path", *dbPath, "err", err)
		os.Exit(1)
	}
	defer st.Close()

	if *seed {
		var base time.Time
		if *seedDay != "" {
			var err error
			base, err = time.Parse("2006-01-02", *seedDay)
			if err != nil {
				fmt.Fprintf(os.Stderr, "teha: -seed-date must be a day such as 2026-08-25: %v\n", err)
				os.Exit(2)
			}
		}
		if err := seedExample(st, base); err != nil {
			log.Error("seed failed", "err", err)
			os.Exit(1)
		}
		fmt.Println("seeded")
		return
	}

	tok := *token
	if tok == "" && !*dev {
		tok = newToken()
		fmt.Fprintf(os.Stderr, "\n  No TEHA_TOKEN was set, so this run uses a new one:\n\n    %s\n\n"+
			"  Export it to keep the same token across restarts.\n\n", tok)
	}
	if *dev {
		tok = ""
		log.Warn("development mode: every request is allowed without a token")
	}

	apiSrv := api.New(st, tok, log)

	mux := http.NewServeMux()
	mux.Handle("/v1/", apiSrv.Routes())
	// Off unless the operator asks for it. A task list is a map of a person's
	// life and work. An always-on agent endpoint turns one leaked token from a
	// read of that map into control over it, so the wider blast radius is opt
	// in. Turn it on with -mcp or TEHA_MCP=1.
	if *mcpOn {
		mcpSrv := mcpsrv.New(st, apiSrv)
		h := withBearer(tok, mcpSrv.HTTP())
		// Both forms. A client that appends a slash must not get a 404 from the
		// web app's catch-all handler and then report a broken server.
		mux.Handle("/mcp", h)
		mux.Handle("/mcp/", h)
		log.Info("the MCP endpoint is on", "path", "/mcp")
	}
	mux.HandleFunc("/login", loginHandler(tok))
	mux.Handle("/", webHandler(tok))

	srv := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(log, mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("teha is listening", "addr", *addr, "db", *dbPath, "auth", tok != "")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("the server stopped", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}

// webHandler serves the web app. An unauthenticated browser goes to the login
// page instead of the app shell.
func webHandler(token string) http.Handler {
	assets := webui.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && token != "" && !hasCookie(r, token) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		assets.ServeHTTP(w, r)
	})
}

// loginHandler takes the device token once and stores it in a cookie, so the
// app and the PWA work without a password manager entry per request.
func loginHandler(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			if r.PostFormValue("token") == token {
				http.SetCookie(w, &http.Cookie{
					Name: "teha_token", Value: token, Path: "/", HttpOnly: true,
					SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil,
					Expires: time.Now().AddDate(1, 0, 0),
				})
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, "/login?bad=1", http.StatusSeeOther)
			return
		}
		data, err := webui.Read("login.html")
		if err != nil {
			http.Error(w, "missing login page", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	}
}

// withBearer guards the MCP endpoint. An MCP client sends a bearer header, so
// no cookie path is needed here.
func withBearer(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == token {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="teha"`)
		http.Error(w, "a token is required", http.StatusUnauthorized)
	})
}

func hasCookie(r *http.Request, token string) bool {
	c, err := r.Cookie("teha_token")
	return err == nil && c.Value == token
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Debug("request", "method", r.Method, "path", r.URL.Path, "ms", time.Since(start).Milliseconds())
	})
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envBool reads a switch from the environment. Anything a person writes to mean
// yes counts, because a deployment file is edited by hand and "true" and "1"
// must not behave differently.
func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func absDir(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
