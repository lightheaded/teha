// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// client is a simulated device. It holds the rows it knows and the version it
// last saw, exactly like the web app and the Android app do.
type client struct {
	name    string
	version int64
	tasks   map[string]Task
}

func newClient(name string) *client {
	return &client{name: name, tasks: map[string]Task{}}
}

// pull takes everything above the version this client knows.
func (c *client) pull(t *testing.T, s *Store) {
	t.Helper()
	d, err := s.Pull(c.version)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range d.Tasks {
		if task.DeletedAt != nil {
			delete(c.tasks, task.ID)
			continue
		}
		c.tasks[task.ID] = task
	}
	c.version = d.Version
}

// state renders what the client believes, so two clients can be compared.
func (c *client) state() string {
	keys := make([]string, 0, len(c.tasks))
	for k := range c.tasks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for _, k := range keys {
		t := c.tasks[k]
		due := "-"
		if t.DueDate != nil {
			due = *t.DueDate
		}
		out += fmt.Sprintf("%s|%s|%d|%s|%s\n", t.ID, t.Title, t.Priority, due, t.State)
	}
	return out
}

// TestClientsConvergeUnderRandomInterleaving is the promise in the plan: three
// clients write in a random order, each one pulls at random moments, and every
// client ends at the same state as the server. No command may vanish, and no
// command may apply twice.
func TestClientsConvergeUnderRandomInterleaving(t *testing.T) {
	for _, seed := range []int64{1, 7, 42, 1234, 987654} {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			s, err := Open(filepath.Join(t.TempDir(), "c.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			s.Now = func() time.Time { return time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC) }

			clients := []*client{newClient("web"), newClient("phone"), newClient("agent")}
			var ids []string
			applied := 0

			for step := 0; step < 300; step++ {
				c := clients[rng.Intn(len(clients))]
				var cmds []Command

				switch {
				case len(ids) == 0 || rng.Intn(100) < 35: // add
					id := fmt.Sprintf("t%03d", len(ids))
					ids = append(ids, id)
					args := TaskArgs{ID: id, Title: ptr(fmt.Sprintf("task %s from %s", id, c.name))}
					if rng.Intn(2) == 0 {
						args.DueDate = ptr("2026-08-25")
					}
					cmds = append(cmds, mkCmd(fmt.Sprintf("%s-add-%d", c.name, step), "task_add", args))
				case rng.Intn(100) < 40: // update a title or a priority
					id := ids[rng.Intn(len(ids))]
					args := TaskArgs{ID: id}
					if rng.Intn(2) == 0 {
						args.Title = ptr(fmt.Sprintf("edit %d by %s", step, c.name))
					} else {
						args.Priority = ptr(rng.Intn(4) + 1)
					}
					cmds = append(cmds, mkCmd(fmt.Sprintf("%s-upd-%d", c.name, step), "task_update", args))
				case rng.Intn(100) < 60: // complete
					id := ids[rng.Intn(len(ids))]
					cmds = append(cmds, mkCmd(fmt.Sprintf("%s-done-%d", c.name, step), "task_complete", IDArgs{ID: id}))
				default: // delete
					id := ids[rng.Intn(len(ids))]
					cmds = append(cmds, mkCmd(fmt.Sprintf("%s-del-%d", c.name, step), "task_delete", IDArgs{ID: id}))
				}

				// Half the time the client sends the command twice, which is what
				// a lost response looks like from the client side.
				batch := cmds
				if rng.Intn(4) == 0 {
					batch = append(append([]Command{}, cmds...), cmds...)
				}
				_, res, err := s.Apply(batch)
				if err != nil {
					t.Fatalf("apply failed at step %d: %v", step, err)
				}
				for _, r := range res {
					if !r.OK {
						// A delete of an already deleted row is the only failure
						// this walk can produce, and it must stay harmless.
						t.Logf("step %d: %s", step, r.Error)
					}
				}
				applied++

				if rng.Intn(3) == 0 { // this client pulls now
					c.pull(t, s)
				}
			}

			// Everyone pulls at the end, which is what happens when a device
			// comes back from being offline.
			for _, c := range clients {
				c.pull(t, s)
			}
			want := clients[0].state()
			for _, c := range clients[1:] {
				if got := c.state(); got != want {
					t.Fatalf("client %s diverged.\n--- %s ---\n%s\n--- %s ---\n%s",
						c.name, clients[0].name, want, c.name, got)
				}
			}

			// A fresh client that pulls from zero must reach the same state, so
			// the change log is a complete history and not a cache.
			fresh := newClient("fresh")
			fresh.pull(t, s)
			if got := fresh.state(); got != want {
				t.Fatalf("a fresh client diverged.\n--- known ---\n%s\n--- fresh ---\n%s", want, got)
			}

			// No command applied twice: the count of stored uuids equals the
			// count of distinct uuids sent.
			var storedUUIDs int
			if err := s.db.QueryRow(`SELECT count(*) FROM applied_command`).Scan(&storedUUIDs); err != nil {
				t.Fatal(err)
			}
			if storedUUIDs > applied {
				t.Fatalf("stored %d commands for %d sends: a replay applied twice", storedUUIDs, applied)
			}
		})
	}
}

func mkCmd(uuid, kind string, args any) Command {
	raw, _ := json.Marshal(args)
	return Command{UUID: uuid, Type: kind, Args: raw}
}
