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
	"github.com/lightheaded/teha/internal/push"
	"github.com/lightheaded/teha/internal/store"
	"github.com/lightheaded/teha/internal/webui"
)

// buildVersion is set at link time by the release job, with
//
//	-ldflags "-X main.buildVersion=<version>"
//
// A build that does not set it says so, rather than claiming a release number
// it does not have. It must stay a string: -X does nothing to any other type,
// and it fails silently, so a flag of the same name would leave every release
// binary reporting the wrong version.
var buildVersion = "dev (no version set at build time)"

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
		rpID    = flag.String("rp-id", os.Getenv("TEHA_RP_ID"), "the WebAuthn relying-party id, a bare domain such as teha.example. Empty reads it from the request host")
		rpOrig  = flag.String("origin", os.Getenv("TEHA_ORIGIN"), "the origin the web app is served from, such as https://teha.example. Empty builds it from the request host")
		fwd     = flag.Bool("trust-forwarded", envBool("TEHA_TRUST_FORWARDED"), "read the client address from X-Forwarded-For. Turn it on only behind a proxy that writes that header, because a client that writes its own address escapes the passkey lockout")
		seed    = flag.Bool("seed", false, "write example data into an empty database and exit")
		seedDay = flag.String("seed-date", "", "the day that seeded dates count from, as 2006-01-02. Empty means today. Used by the screenshot job, so that an unchanged screen produces an identical image on any day")
		version = flag.Bool("version", false, "print the version and exit")
		// Web Push, per docs/DECISIONS.md D-003. The public key has a flag,
		// because it is not a secret. The private key has NO flag on purpose: a
		// command argument is visible in the process list to every other process
		// on the machine, so it arrives from the environment alone.
		vapidGen  = flag.Bool("vapid-keys", false, "make a VAPID keypair, print it and exit. The private key is a secret: put it in the encrypted store, never in the repository")
		vapidPub  = flag.String("vapid-public", os.Getenv("TEHA_VAPID_PUBLIC_KEY"), "the VAPID public key. Push stays off without it and without TEHA_VAPID_PRIVATE_KEY")
		vapidSub  = flag.String("vapid-subject", envOr("TEHA_VAPID_SUBJECT", "https://github.com/lightheaded/teha"), "the VAPID subject: a mailto: address or an https: URL that a push service can use to reach the operator")
		pushEvery = flag.Duration("push-interval", 30*time.Second, "how often the reminder scheduler looks for due reminders")
		ckptEvery = flag.Duration("checkpoint-interval", 10*time.Second, "how often the write-ahead log is written into the database file. This is what a backup replicates from: see scripts/restore-drill.sh. Zero turns it off")
	)
	flag.Parse()

	if *version {
		fmt.Println("teha " + buildVersion)
		return
	}

	if *vapidGen {
		os.Exit(printVAPIDKeys())
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
	// The relying party comes from configuration, and from the request host
	// when configuration says nothing. No hostname belongs in this binary.
	apiSrv.RP = api.RelyingParty{ID: *rpID, Origin: *rpOrig, DisplayName: "teha"}
	apiSrv.TrustForwarded = *fwd

	// The reminder scheduler. Both keys are necessary: a public key alone
	// cannot sign, and a private key alone gives the browser nothing to
	// subscribe with. Without them the server runs exactly as before and the
	// settings area says that push is off.
	var sender *push.Sender
	vapidPriv := os.Getenv("TEHA_VAPID_PRIVATE_KEY")
	switch {
	case *vapidPub != "" && vapidPriv != "":
		sender = push.New(st, push.Keys{Public: *vapidPub, Private: vapidPriv, Subject: *vapidSub}, log)
		sender.Interval = *pushEvery
		sender.Notify = apiSrv.Notify
		apiSrv.Push = sender
	case *vapidPub != "" || vapidPriv != "":
		log.Warn("push needs both keys, so it stays off",
			"have_public", *vapidPub != "", "have_private", vapidPriv != "")
	default:
		log.Info("push is off: no VAPID keys. Run `teha -vapid-keys` to make a pair")
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/", apiSrv.Routes())
	// Off unless the operator asks for it. A task list is a map of a person's
	// life and work. An always-on agent endpoint turns one leaked token from a
	// read of that map into control over it, so the wider blast radius is opt
	// in. Turn it on with -mcp or TEHA_MCP=1.
	if *mcpOn {
		mcpSrv := mcpsrv.New(st, apiSrv)
		// The same rule as every other route: the token names the account, so
		// an invited person can drive an agent against their own lists.
		h := apiSrv.GuardHandler(mcpSrv.HTTP())
		// Both forms. A client that appends a slash must not get a 404 from the
		// web app's catch-all handler and then report a broken server.
		mux.Handle("/mcp", h)
		mux.Handle("/mcp/", h)
		log.Info("the MCP endpoint is on", "path", "/mcp")
	}
	mux.HandleFunc("/login", loginHandler(tok))
	mux.Handle("/", webHandler(apiSrv))

	srv := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(log, mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if sender != nil {
		go sender.Run(ctx)
	}

	// The checkpoint loop. A backup replicates the database file, and one
	// long-lived connection leaves a write in the write-ahead log until SQLite
	// decides to move it, which is a matter of megabytes and not of seconds.
	// This bounds what a restore can lose to one interval. Proven by
	// scripts/restore-drill.sh, which fails without it.
	if *ckptEvery > 0 {
		go func() {
			t := time.NewTicker(*ckptEvery)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if err := st.Checkpoint(); err != nil {
						log.Warn("the checkpoint did not run", "err", err)
					}
				}
			}
		}()
	}

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
	// One last checkpoint, so a planned stop leaves nothing in the log for a
	// backup to miss.
	if err := st.Checkpoint(); err != nil {
		log.Warn("the last checkpoint did not run", "err", err)
	}
}

// webHandler serves the web app. An unauthenticated browser goes to the login
// page instead of the app shell.
//
// The API server answers the question, because there are now two ways in: the
// device token in a cookie, and a passkey session.
func webHandler(apiSrv *api.Server) http.Handler {
	assets := webui.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && !apiSrv.Authenticated(r) {
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
				// Secure follows the host, not r.TLS. The server usually sits
				// behind a proxy that ends TLS, so r.TLS is nil on a request
				// that reached the browser over https, and the old rule left
				// the token cookie without the flag on every such deployment.
				http.SetCookie(w, &http.Cookie{
					Name: "teha_token", Value: token, Path: "/", HttpOnly: true,
					SameSite: http.SameSiteLaxMode, Secure: api.SecureCookie(r),
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
