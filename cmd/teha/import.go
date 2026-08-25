// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lightheaded/teha/internal/store"
	"github.com/lightheaded/teha/internal/todoist"
)

// runImport reads a whole Todoist account and writes it into the store.
//
// The command is safe to run again: every command carries a uuid that comes
// from the Todoist id, so a second run changes nothing.
func runImport(args []string) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: teha import --token <todoist token> [--db teha.db] [--dry-run]\n\n")
		fs.PrintDefaults()
	}
	var (
		// The token has no default from a flag file. TODOIST_TOKEN keeps it out
		// of the shell history.
		token   = fs.String("token", os.Getenv("TODOIST_TOKEN"), "Todoist API token. TODOIST_TOKEN also works")
		dbPath  = fs.String("db", envOr("TEHA_DB", "teha.db"), "path to the SQLite file")
		dryRun  = fs.Bool("dry-run", false, "read Todoist, print the summary, and write nothing")
		timeout = fs.Duration("timeout", 15*time.Minute, "limit for the whole read")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "A Todoist API token is necessary. Give --token or set TODOIST_TOKEN.")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	client := todoist.New(*token)
	// A test or a rehearsal can point the importer at a recorded payload, so a
	// person checks the mapping before the real account is read.
	if endpoint := os.Getenv("TEHA_TODOIST_ENDPOINT"); endpoint != "" {
		client.Endpoint = endpoint
		fmt.Println("The endpoint comes from TEHA_TODOIST_ENDPOINT, not from Todoist.")
	}
	fmt.Println("Reading the Todoist account. One full sync is enough for a personal account.")
	data, err := client.Sync(ctx, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "The read from Todoist failed:", err)
		return 1
	}
	fmt.Printf("Todoist sent: projects %d, tasks %d, labels %d, sections %d, comments %d.\n",
		len(data.Projects), len(data.Items), len(data.Labels), len(data.Sections), len(data.Notes))

	st, err := openForImport(*dbPath, *dryRun)
	if err != nil {
		fmt.Fprintln(os.Stderr, "The database did not open:", err)
		return 1
	}
	if st != nil {
		defer st.Close()
	}

	sum, err := todoist.Import(st, data, todoist.Options{DryRun: *dryRun, Requests: client.Requests})
	if err != nil {
		fmt.Fprintln(os.Stderr, "The import failed:", err)
		return 1
	}
	sum.Write(os.Stdout)
	if sum.Failed > 0 {
		return 1
	}
	return 0
}

// openForImport opens the store. A dry run against a database file that does
// not exist opens nothing, because store.Open creates the file.
func openForImport(path string, dryRun bool) (*store.Store, error) {
	if dryRun {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			fmt.Printf("The file %s does not exist. The dry run counts every row as new.\n", path)
			return nil, nil
		}
	}
	return store.Open(path)
}
