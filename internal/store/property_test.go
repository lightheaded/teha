// SPDX-License-Identifier: AGPL-3.0-or-later

package store

// The property test for the sync promises of docs/PLAN.md §6.1.
//
// docs/PLAN.md §9 says that every user promise is a test. §6.1 makes seven
// promises about sync, and this file holds one property for each of them:
//
//	convergence     two clients that apply one set of commands in different
//	                orders reach the same state, and a replay changes nothing
//	idempotence     a command with a uuid that the store saw before applies
//	                once, and a restart does not forget that
//	no lost edit    every accepted command is in the change log, and a client
//	                that pulls from any version it stopped at catches up fully
//	monotone        the version counter never goes backwards and never repeats
//	per field       two clients that edit different fields of one task both
//	                keep their edit
//	fractional      the order key of a repeated insertion keeps a strict order
//	                (package order holds that property test)
//	temporary ids   a command that names a row created earlier in the same
//	                batch resolves
//
// A generator builds the command stream from a seed, so a failure repeats.
// Every failure message carries the seed. The clock is injected, never read.

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// propertyClock is the only clock in this file. A generated stream must give
// the same answer on every machine and in every time zone, so nothing here
// reads the wall clock.
var propertyClock = time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)

func openStoreAt(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatalf("the store at %s did not open: %v", path, err)
	}
	s.Now = func() time.Time { return propertyClock }
	t.Cleanup(func() { s.Close() })
	return s
}

// seeds returns the seeds of one run.
//
// The first seeds are fixed, so a failure in CI is the same failure on a
// laptop. The last seed comes from the clock, so a long run with -count finds
// what a fixed corpus cannot. Every seed goes into the log, and every failure
// names the seed that produced it, so TEHA_SEED repeats one case exactly.
//
// A short run (-short) keeps two fixed seeds and a smaller stream, because CI
// runs this on every commit and a test nobody can afford to run is a test
// nobody runs.
func seeds(t *testing.T) []int64 {
	t.Helper()
	if v := os.Getenv("TEHA_SEED"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			t.Fatalf("TEHA_SEED=%q is not a number: %v", v, err)
		}
		t.Logf("one seed from TEHA_SEED: %d", n)
		return []int64{n}
	}
	out := []int64{1, 7, 42, 1234, 987654}
	if testing.Short() {
		out = out[:2]
	}
	out = append(out, time.Now().UnixNano())
	t.Logf("seeds %v. To repeat one: TEHA_SEED=<seed> go test -run %s ./internal/store", out, t.Name())
	return out
}

// streamSize returns how many tasks one generated stream holds.
func streamSize() int {
	if testing.Short() {
		return 20
	}
	return 60
}

// --- the state renderer -----------------------------------------------------

// snapshot renders every row of the store as text, so two stores compare
// character by character and a failure reads like a diff.
//
// It leaves out the version and the timestamps. Those count the writes: two
// stores that took the same writes in a different order count them
// differently while they hold the same data.
//
// A label compares by name and not by id, because setLabels invents an id for
// a label that a task names inline, and two stores invent different ids for
// the same name. The name is what the task carries and what a person reads.
func snapshot(t *testing.T, s *Store) string {
	t.Helper()
	d, err := s.Pull(0)
	if err != nil {
		t.Fatalf("Pull(0) failed: %v", err)
	}
	return renderDelta(d)
}

func renderDelta(d Delta) string {
	var b strings.Builder

	ps := append([]Project{}, d.Projects...)
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
	for _, p := range ps {
		fmt.Fprintf(&b, "project %s name=%q color=%s parent=%s order=%s inbox=%v gone=%v\n",
			p.ID, p.Name, p.Color, text(p.ParentID), p.OrderKey, p.IsInbox, p.DeletedAt != nil)
	}

	ls := append([]Label{}, d.Labels...)
	sort.Slice(ls, func(i, j int) bool {
		if ls[i].Name != ls[j].Name {
			return ls[i].Name < ls[j].Name
		}
		// Two rows can share one name, so the render needs a second key or the
		// sort is arbitrary and a comparison of two stores fails at random.
		return (ls[i].DeletedAt != nil) != (ls[j].DeletedAt != nil) && ls[i].DeletedAt == nil
	})
	for _, l := range ls {
		fmt.Fprintf(&b, "label %q color=%s gone=%v\n", l.Name, l.Color, l.DeletedAt != nil)
	}

	ts := append([]Task{}, d.Tasks...)
	sort.Slice(ts, func(i, j int) bool { return ts[i].ID < ts[j].ID })
	for _, task := range ts {
		labels := append([]string{}, task.Labels...)
		sort.Strings(labels)
		fmt.Fprintf(&b, "task %s project=%s parent=%s order=%s title=%q desc=%q p=%d "+
			"due=%s time=%s tz=%s rrule=%s fromdone=%v start=%s deadline=%s dur=%s "+
			"state=%s done=%v gone=%v src=%s labels=%s\n",
			task.ID, task.ProjectID, text(task.ParentID), task.OrderKey, task.Title,
			task.Description, task.Priority, text(task.DueDate), text(task.DueTime),
			text(task.DueTz), text(task.RRule), task.FromComplete, text(task.StartDate),
			text(task.Deadline), number(task.DurationMin), task.State,
			task.CompletedAt != nil, task.DeletedAt != nil, text(task.SourceRef),
			strings.Join(labels, ","))
	}
	return b.String()
}

func text(p *string) string {
	if p == nil {
		return "-"
	}
	return *p
}

func number(p *int) string {
	if p == nil {
		return "-"
	}
	return strconv.Itoa(*p)
}

// --- the generator ----------------------------------------------------------

// step is one command plus the rows it needs. The needs are what make a random
// order legal: a command never runs before the command that makes a row it
// names.
type step struct {
	cmd   Command
	makes string
	needs []string
}

// scenario is one generated command stream.
type scenario struct {
	steps    []step
	projects []string
	tasks    []string
}

func (sc *scenario) add(s step) { sc.steps = append(sc.steps, s) }

// order returns a random legal order of the whole stream. Two calls with two
// different generators give two orders of the same set of commands, which is
// what the convergence property needs.
func (sc *scenario) order(rng *rand.Rand) []Command {
	left := append([]step{}, sc.steps...)
	live := map[string]bool{InboxID: true}
	out := make([]Command, 0, len(left))
	for len(left) > 0 {
		var ready []int
		for i, s := range left {
			ok := true
			for _, need := range s.needs {
				if !live[need] {
					ok = false
					break
				}
			}
			if ok {
				ready = append(ready, i)
			}
		}
		if len(ready) == 0 {
			panic("the generated scenario has a cycle")
		}
		pick := ready[rng.Intn(len(ready))]
		s := left[pick]
		out = append(out, s.cmd)
		if s.makes != "" {
			live[s.makes] = true
		}
		left = append(left[:pick], left[pick+1:]...)
	}
	return out
}

// generate builds a stream that commutes: every command writes fields that no
// other command in the stream writes. Last write wins by server receipt order,
// so two writes to one field are order dependent by design and must stay out
// of a commutativity test. Two writes to two fields of one row stay in, and
// that is the invariant most likely to break.
//
// Two things stay out of the stream on purpose:
//
//   - A repeat rule. task_complete moves a repeating task to its next date, so
//     a completion and a date edit write the same field. TestRecurringComplete
//     Advances in store_test.go covers the rule itself.
//   - A label_add whose name a task also names inline. The two orders give one
//     label row or two, because setLabels matches a label by name while
//     label_add trusts the id it is given. docs/BACKLOG.md records that hole.
func generate(rng *rand.Rand, taskCount int) *scenario {
	sc := &scenario{}
	seq := 0
	uuid := func(kind string) string {
		seq++
		return fmt.Sprintf("%s-%04d", kind, seq)
	}
	keys := []string{"C", "F", "V", "k", "s"}
	key := func() *string { return ptr(keys[rng.Intn(len(keys))] + strconv.Itoa(rng.Intn(9)+1)) }

	// Projects, and a sub-project under some of them.
	for i := 0; i < 1+rng.Intn(3); i++ {
		root := fmt.Sprintf("p_root%d", i)
		sc.add(step{makes: root, cmd: mkCmd(uuid("proj"), "project_add", ProjectArgs{
			ID: root, Name: ptr(fmt.Sprintf("Project %d", i)), OrderKey: key(),
		})})
		sc.projects = append(sc.projects, root)
		if rng.Intn(2) == 0 {
			sub := root + "_sub"
			sc.add(step{makes: sub, needs: []string{root}, cmd: mkCmd(uuid("proj"), "project_add", ProjectArgs{
				ID: sub, Name: ptr("Sub of project " + strconv.Itoa(i)), ParentID: ptr(root), OrderKey: key(),
			})})
			sc.projects = append(sc.projects, sub)
		}
	}

	// Labels that a command names by id. The names never meet the inline names
	// that a task carries, because setLabels matches a label by name.
	var ownLabels []string
	for i := 0; i < 2+rng.Intn(3); i++ {
		name := fmt.Sprintf("own%d", i)
		id := "l_" + name
		sc.add(step{makes: id, cmd: mkCmd(uuid("label"), "label_add", LabelArgs{ID: id, Name: ptr(name)})})
		ownLabels = append(ownLabels, name)
	}
	// One label is only ever deleted, never named by an edit. A task_update
	// that names a label after a label_delete makes a second row with the same
	// name, because setLabels looks for a live label and finds none. That is a
	// real hole, docs/BACKLOG.md records it, and it is not this property.
	doomed := ownLabels[len(ownLabels)-1]
	ownLabels = ownLabels[:len(ownLabels)-1]

	// Tasks.
	for i := 0; i < taskCount; i++ {
		id := fmt.Sprintf("t_%04d", i)
		args := TaskArgs{ID: id, Title: ptr(fmt.Sprintf("task %d", i)), OrderKey: key()}
		var needs []string
		if rng.Intn(2) == 0 {
			p := sc.projects[rng.Intn(len(sc.projects))]
			args.ProjectID = ptr(p)
			needs = append(needs, p)
		}
		if len(sc.tasks) > 0 && rng.Intn(5) == 0 {
			parent := sc.tasks[rng.Intn(len(sc.tasks))]
			args.ParentID = ptr(parent)
			needs = append(needs, parent)
		}
		if rng.Intn(3) == 0 {
			args.Labels = []string{fmt.Sprintf("inline%d", rng.Intn(3))}
		}
		if rng.Intn(2) == 0 {
			args.DueDate = ptr(propertyClock.AddDate(0, 0, rng.Intn(20)-5).Format("2006-01-02"))
		}
		if rng.Intn(3) == 0 {
			args.Priority = ptr(1 + rng.Intn(4))
		}
		sc.add(step{makes: id, needs: needs, cmd: mkCmd(uuid("add"), "task_add", args)})
		sc.tasks = append(sc.tasks, id)
	}

	// Edits. Each one claims a field of a row, and a field is claimed once.
	claimed := map[string]bool{}
	claim := func(row, field string) bool {
		k := row + "/" + field
		if claimed[k] {
			return false
		}
		claimed[k] = true
		return true
	}
	fields := []string{"title", "description", "priority", "due_date", "due_time", "due_tz",
		"start_date", "deadline", "duration_min", "order_key", "source_ref", "labels", "project_id"}

	for i := 0; i < taskCount*2; i++ {
		id := sc.tasks[rng.Intn(len(sc.tasks))]
		switch n := rng.Intn(10); {
		case n < 7: // a field edit
			field := fields[rng.Intn(len(fields))]
			if !claim(id, field) {
				continue
			}
			args := TaskArgs{ID: id}
			needs := []string{id}
			switch field {
			case "title":
				args.Title = ptr(fmt.Sprintf("edit %d", i))
			case "description":
				args.Description = ptr(fmt.Sprintf("note %d", i))
			case "priority":
				args.Priority = ptr(1 + rng.Intn(4))
			case "due_date":
				args.DueDate = ptr(propertyClock.AddDate(0, 0, rng.Intn(30)).Format("2006-01-02"))
			case "due_time":
				args.DueTime = ptr(fmt.Sprintf("%02d:%02d", rng.Intn(24), rng.Intn(60)))
			case "due_tz":
				args.DueTz = ptr("Europe/Tallinn")
			case "start_date":
				args.StartDate = ptr(propertyClock.AddDate(0, 0, -rng.Intn(10)).Format("2006-01-02"))
			case "deadline":
				args.Deadline = ptr(propertyClock.AddDate(0, 0, 30+rng.Intn(30)).Format("2006-01-02"))
			case "duration_min":
				args.DurationMin = ptr(15 * (1 + rng.Intn(8)))
			case "order_key":
				args.OrderKey = key()
			case "source_ref":
				args.SourceRef = ptr(fmt.Sprintf("todoist:%d", 90000+i))
			case "labels":
				name := ownLabels[rng.Intn(len(ownLabels))]
				args.Labels = []string{name}
				// The label row has to exist first. setLabels matches a label
				// by name and label_add trusts the id it is given, so a
				// task_update that names a label before its label_add arrives
				// makes a second row with the same name. That is a real hole
				// and docs/BACKLOG.md records it, but it is not the property
				// under test here.
				needs = append(needs, "l_"+name)
			case "project_id":
				p := sc.projects[rng.Intn(len(sc.projects))]
				args.ProjectID = ptr(p)
				needs = append(needs, p)
			}
			sc.add(step{needs: needs, cmd: mkCmd(uuid("upd"), "task_update", args)})
		case n < 9: // one state change per task
			if !claim(id, "state") {
				continue
			}
			kind := []string{"task_complete", "task_wont_do", "task_uncomplete"}[rng.Intn(3)]
			sc.add(step{needs: []string{id}, cmd: mkCmd(uuid("state"), kind, IDArgs{ID: id})})
		default: // one delete or restore per task
			if !claim(id, "deleted_at") {
				continue
			}
			kind := "task_delete"
			if rng.Intn(4) == 0 {
				kind = "task_restore"
			}
			sc.add(step{needs: []string{id}, cmd: mkCmd(uuid("del"), kind, IDArgs{ID: id})})
		}
	}

	// A project edit and a label delete, one field each.
	for _, p := range sc.projects {
		if p == InboxID || rng.Intn(2) == 1 {
			continue
		}
		field := []string{"name", "color", "order_key"}[rng.Intn(3)]
		if !claim(p, field) {
			continue
		}
		args := ProjectArgs{ID: p}
		switch field {
		case "name":
			args.Name = ptr("Renamed " + p)
		case "color":
			args.Color = ptr("green")
		case "order_key":
			args.OrderKey = key()
		}
		sc.add(step{needs: []string{p}, cmd: mkCmd(uuid("projupd"), "project_update", args)})
	}
	if rng.Intn(2) == 0 {
		sc.add(step{needs: []string{"l_" + doomed}, cmd: mkCmd(uuid("labeldel"), "label_delete", IDArgs{ID: "l_" + doomed})})
	}
	return sc
}

// --- the properties ---------------------------------------------------------

// TestPropertyCommandsCommute is the convergence promise. One set of commands
// goes into two stores in two different orders. Both stores must hold the same
// rows. A third pass of the same commands must change nothing at all.
func TestPropertyCommandsCommute(t *testing.T) {
	for _, seed := range seeds(t) {
		seed := seed
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			sc := generate(rng, streamSize())

			first := sc.order(rand.New(rand.NewSource(seed*31 + 1)))
			second := sc.order(rand.New(rand.NewSource(seed*31 + 2)))
			if len(first) != len(second) {
				t.Fatalf("seed %d: the two orders hold %d and %d commands", seed, len(first), len(second))
			}
			if samePlace := countSamePlace(first, second); samePlace == len(first) {
				t.Logf("seed %d: the two orders came out identical, so this case only tests a replay", seed)
			}

			dir := t.TempDir()
			one := openStoreAt(t, filepath.Join(dir, "one.db"))
			two := openStoreAt(t, filepath.Join(dir, "two.db"))

			applyInBatches(t, one, first, rng, seed)
			applyInBatches(t, two, second, rng, seed)

			left, right := snapshot(t, one), snapshot(t, two)
			if left != right {
				t.Fatalf("seed %d: two orders of %d commands gave two states.\n%s",
					seed, len(first), firstDifference(left, right))
			}
			checkInvariants(t, one, fmt.Sprintf("seed %d, first order", seed))
			checkInvariants(t, two, fmt.Sprintf("seed %d, second order", seed))

			// A third pass of the same commands changes nothing, because every
			// uuid is already in applied_command.
			versionBefore := version(t, one)
			if _, _, err := one.Apply(first); err != nil {
				t.Fatalf("seed %d: the replay failed: %v", seed, err)
			}
			if got := snapshot(t, one); got != left {
				t.Fatalf("seed %d: a replay of the same commands changed the state.\n%s",
					seed, firstDifference(left, got))
			}
			if got := version(t, one); got != versionBefore {
				t.Fatalf("seed %d: a replay moved the version from %d to %d", seed, versionBefore, got)
			}
		})
	}
}

// TestPropertyResendSurvivesARestart is the idempotence promise across a
// process restart. applied_command is a table and not a cache, so a store that
// closed and opened again still refuses a command it applied before.
func TestPropertyResendSurvivesARestart(t *testing.T) {
	for _, seed := range seeds(t) {
		seed := seed
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			sc := generate(rng, streamSize())
			cmds := sc.order(rand.New(rand.NewSource(seed)))

			path := filepath.Join(t.TempDir(), "restart.db")
			s := openStoreAt(t, path)
			applyInBatches(t, s, cmds, rng, seed)
			before := snapshot(t, s)
			versionBefore := version(t, s)
			accepted := acceptedCount(t, s)
			if err := s.Close(); err != nil {
				t.Fatalf("seed %d: close failed: %v", seed, err)
			}

			again := openStoreAt(t, path)
			if got := snapshot(t, again); got != before {
				t.Fatalf("seed %d: the reopened store holds another state.\n%s",
					seed, firstDifference(before, got))
			}
			// The whole stream comes back, which is what an outbox that never
			// saw a response does after the phone restarts.
			if _, res, err := again.Apply(cmds); err != nil {
				t.Fatalf("seed %d: the resend failed: %v", seed, err)
			} else {
				for _, r := range res {
					if !r.OK {
						t.Errorf("seed %d: a resend of %s failed: %s", seed, r.UUID, r.Error)
					}
				}
			}
			if got := snapshot(t, again); got != before {
				t.Fatalf("seed %d: a resend after a restart changed the state.\n%s",
					seed, firstDifference(before, got))
			}
			if got := version(t, again); got != versionBefore {
				t.Fatalf("seed %d: a resend after a restart moved the version from %d to %d",
					seed, versionBefore, got)
			}
			if got := acceptedCount(t, again); got != accepted {
				t.Fatalf("seed %d: applied_command grew from %d to %d on a resend", seed, accepted, got)
			}
		})
	}
}

// TestPropertyVersionIsMonotone is the version promise. The counter never goes
// backwards, it never repeats, and it moves if and only if a command was
// accepted. A batch of pure resends must leave it still, because an SSE event
// on a version that carries no row wakes every client for nothing.
func TestPropertyVersionIsMonotone(t *testing.T) {
	for _, seed := range seeds(t) {
		seed := seed
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			sc := generate(rng, streamSize())
			cmds := sc.order(rand.New(rand.NewSource(seed)))
			s := openStoreAt(t, filepath.Join(t.TempDir(), "monotone.db"))

			seen := map[int64]bool{}
			last := version(t, s)
			for from := 0; from < len(cmds); {
				size := 1 + rng.Intn(6)
				to := from + size
				if to > len(cmds) {
					to = len(cmds)
				}
				batch := cmds[from:to]
				resent := rng.Intn(3) == 0
				if resent { // send the batch twice inside one call
					batch = append(append([]Command{}, batch...), batch...)
				}
				before := acceptedCount(t, s)
				v, res, err := s.Apply(batch)
				if err != nil {
					t.Fatalf("seed %d: Apply failed at %d: %v", seed, from, err)
				}
				after := acceptedCount(t, s)

				if v < last {
					t.Fatalf("seed %d: the version went backwards from %d to %d", seed, last, v)
				}
				if after > before && v == last {
					t.Fatalf("seed %d: %d commands were accepted and the version stayed at %d",
						seed, after-before, v)
				}
				if after == before && v != last {
					t.Fatalf("seed %d: nothing was accepted and the version moved from %d to %d",
						seed, last, v)
				}
				if v != last && seen[v] {
					t.Fatalf("seed %d: the version %d came back", seed, v)
				}
				seen[v] = true
				last = v

				// Now the same batch again as a separate call, which is what a
				// lost response looks like. It must move nothing.
				if resent {
					v2, _, err := s.Apply(batch)
					if err != nil {
						t.Fatalf("seed %d: the resend failed: %v", seed, err)
					}
					if v2 != v {
						t.Fatalf("seed %d: a resend moved the version from %d to %d", seed, v, v2)
					}
					if got := acceptedCount(t, s); got != after {
						t.Fatalf("seed %d: a resend accepted %d more commands", seed, got-after)
					}
				}
				_ = res
				from = to
			}
			checkInvariants(t, s, fmt.Sprintf("seed %d", seed))
		})
	}
}

// TestPropertyLastWriteWinsPerField is the invariant most likely to be broken,
// so it runs over every pair of fields in both orders. Two clients edit two
// different fields of one task while both are offline. Whatever the order the
// server sees, both edits must survive. A store that wrote whole rows would
// keep only the second one.
func TestPropertyLastWriteWinsPerField(t *testing.T) {
	type edit struct {
		name  string
		apply func(*TaskArgs)
		check func(Task) string // returns what it reads, for the message
		want  string
	}
	edits := []edit{
		{"title", func(a *TaskArgs) { a.Title = ptr("the web edited this") },
			func(t Task) string { return t.Title }, "the web edited this"},
		{"description", func(a *TaskArgs) { a.Description = ptr("a note from the phone") },
			func(t Task) string { return t.Description }, "a note from the phone"},
		{"priority", func(a *TaskArgs) { a.Priority = ptr(1) },
			func(t Task) string { return strconv.Itoa(t.Priority) }, "1"},
		{"due_date", func(a *TaskArgs) { a.DueDate = ptr("2026-09-09") },
			func(t Task) string { return text(t.DueDate) }, "2026-09-09"},
		{"due_time", func(a *TaskArgs) { a.DueTime = ptr("07:45") },
			func(t Task) string { return text(t.DueTime) }, "07:45"},
		{"due_tz", func(a *TaskArgs) { a.DueTz = ptr("Europe/Tallinn") },
			func(t Task) string { return text(t.DueTz) }, "Europe/Tallinn"},
		{"rrule", func(a *TaskArgs) { a.RRule = ptr("FREQ=WEEKLY;BYDAY=MO") },
			func(t Task) string { return text(t.RRule) }, "FREQ=WEEKLY;BYDAY=MO"},
		{"start_date", func(a *TaskArgs) { a.StartDate = ptr("2026-08-20") },
			func(t Task) string { return text(t.StartDate) }, "2026-08-20"},
		{"deadline", func(a *TaskArgs) { a.Deadline = ptr("2026-10-01") },
			func(t Task) string { return text(t.Deadline) }, "2026-10-01"},
		{"duration_min", func(a *TaskArgs) { a.DurationMin = ptr(45) },
			func(t Task) string { return number(t.DurationMin) }, "45"},
		{"order_key", func(a *TaskArgs) { a.OrderKey = ptr("Vk") },
			func(t Task) string { return t.OrderKey }, "Vk"},
		{"source_ref", func(a *TaskArgs) { a.SourceRef = ptr("todoist:12345") },
			func(t Task) string { return text(t.SourceRef) }, "todoist:12345"},
		{"labels", func(a *TaskArgs) { a.Labels = []string{"errand"} },
			func(t Task) string { return strings.Join(t.Labels, ",") }, "errand"},
	}

	for i := range edits {
		for j := range edits {
			if i == j {
				continue
			}
			web, phone := edits[i], edits[j]
			name := web.name + "-then-" + phone.name
			t.Run(name, func(t *testing.T) {
				for _, reversed := range []bool{false, true} {
					s := openStoreAt(t, filepath.Join(t.TempDir(), "field.db"))
					if _, res, err := s.Apply([]Command{
						mkCmd("seed", "task_add", TaskArgs{ID: "t_1", Title: ptr("a shared task")}),
					}); err != nil || !res[0].OK {
						t.Fatalf("the task did not go in: %v %+v", err, res)
					}

					a, b := web, phone
					if reversed {
						a, b = phone, web
					}
					first := TaskArgs{ID: "t_1"}
					a.apply(&first)
					second := TaskArgs{ID: "t_1"}
					b.apply(&second)

					// Two separate calls, because two offline clients arrive at
					// two different moments.
					for n, args := range []TaskArgs{first, second} {
						_, res, err := s.Apply([]Command{mkCmd(fmt.Sprintf("edit-%d", n), "task_update", args)})
						if err != nil {
							t.Fatalf("Apply failed: %v", err)
						}
						if !res[0].OK {
							t.Fatalf("the edit of %s failed: %s", []string{a.name, b.name}[n], res[0].Error)
						}
					}

					task, err := s.Task("t_1")
					if err != nil {
						t.Fatalf("the task vanished: %v", err)
					}
					for _, e := range []edit{a, b} {
						if got := e.check(task); got != e.want {
							t.Errorf("%s arrived first: the %s edit reads %q, want %q. "+
								"A write of the whole row lost the earlier edit",
								a.name, e.name, got, e.want)
						}
					}
					// The title must never change unless one of the two edits
					// wrote it, which is the row-level check.
					if web.name != "title" && phone.name != "title" && task.Title != "a shared task" {
						t.Errorf("the title changed to %q, and neither edit named it", task.Title)
					}
				}
			})
		}
	}
}

// TestPropertyTemporaryIDsResolveInsideOneBatch is the temporary id promise.
//
// §8a of docs/PLAN.md records that a client picks the row id itself, so the
// temporary id and the real id are the same string and no server side mapping
// exists. The property that matters is therefore the one a client depends on:
// a command that names a row created earlier in the same batch resolves, even
// when the whole family is new.
func TestPropertyTemporaryIDsResolveInsideOneBatch(t *testing.T) {
	s := openStoreAt(t, filepath.Join(t.TempDir(), "temp.db"))

	// One batch: a new project, a new sub-project inside it, a task in the
	// sub-project, a sub-task of that task, a sub-sub-task, a label, and an
	// edit of every one of them. Nothing here exists before the batch.
	cmds := []Command{
		mkCmd("b1", "project_add", ProjectArgs{ID: "p_new", Name: ptr("Trip"), OrderKey: ptr("V")}),
		mkCmd("b2", "project_add", ProjectArgs{ID: "p_kid", Name: ptr("Trip packing"), ParentID: ptr("p_new"), OrderKey: ptr("k")}),
		mkCmd("b3", "label_add", LabelArgs{ID: "l_new", Name: ptr("ferry")}),
		mkCmd("b4", "task_add", TaskArgs{ID: "t_root", Title: ptr("Book the ferry"), ProjectID: ptr("p_kid"), Labels: []string{"ferry"}}),
		mkCmd("b5", "task_add", TaskArgs{ID: "t_kid", Title: ptr("Check the timetable"), ProjectID: ptr("p_kid"), ParentID: ptr("t_root")}),
		mkCmd("b6", "task_add", TaskArgs{ID: "t_grandkid", Title: ptr("Print the ticket"), ProjectID: ptr("p_kid"), ParentID: ptr("t_kid")}),
		mkCmd("b7", "task_update", TaskArgs{ID: "t_grandkid", Priority: ptr(1)}),
		// A task that names its project by name, where that project is also new
		// in this batch. This is the path the importer and MCP take.
		mkCmd("b8", "task_add", TaskArgs{ID: "t_byname", Title: ptr("Pack the bags"), Project: ptr("Trip packing")}),
		mkCmd("b9", "task_complete", IDArgs{ID: "t_kid"}),
	}
	_, res, err := s.Apply(cmds)
	if err != nil {
		t.Fatalf("the batch failed: %v", err)
	}
	for _, r := range res {
		if !r.OK {
			t.Fatalf("command %s failed: %s", r.UUID, r.Error)
		}
	}

	root, err := s.Task("t_root")
	if err != nil {
		t.Fatal(err)
	}
	if root.ProjectID != "p_kid" {
		t.Errorf("the task is in project %q, want the sub-project made in the same batch", root.ProjectID)
	}
	if len(root.Labels) != 1 || root.Labels[0] != "ferry" {
		t.Errorf("labels of the root task = %v, want [ferry]", root.Labels)
	}
	kid, err := s.Task("t_kid")
	if err != nil {
		t.Fatal(err)
	}
	if kid.ParentID == nil || *kid.ParentID != "t_root" {
		t.Errorf("parent of the sub-task = %v, want t_root", kid.ParentID)
	}
	if kid.State != "done" {
		t.Errorf("the sub-task was completed in the same batch, state = %q", kid.State)
	}
	grand, err := s.Task("t_grandkid")
	if err != nil {
		t.Fatal(err)
	}
	if grand.ParentID == nil || *grand.ParentID != "t_kid" {
		t.Errorf("parent of the third level = %v, want t_kid", grand.ParentID)
	}
	if grand.Priority != 1 {
		t.Errorf("the edit in the same batch did not land: priority = %d", grand.Priority)
	}
	byName, err := s.Task("t_byname")
	if err != nil {
		t.Fatal(err)
	}
	if byName.ProjectID != "p_kid" {
		t.Errorf("the project name resolved to %q, want p_kid", byName.ProjectID)
	}

	// The whole batch a second time must change nothing.
	before := snapshot(t, s)
	versionBefore := version(t, s)
	if _, _, err := s.Apply(cmds); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, s); got != before {
		t.Fatalf("a resend of the batch changed the state.\n%s", firstDifference(before, got))
	}
	if got := version(t, s); got != versionBefore {
		t.Errorf("a resend of the batch moved the version from %d to %d", versionBefore, got)
	}
	checkInvariants(t, s, "the temporary id batch")
}

// TestPropertyEveryDeviceCatchesUp is the no lost edit promise, from the side
// of a device. Three devices write, each one pulls at its own moments, one of
// them stays offline for a long stretch, and a quarter of the batches arrive
// twice. Every device, and a device that starts from nothing, must end at the
// state the server holds. The comparison covers every table and every field,
// not the five columns the earlier test compared.
func TestPropertyEveryDeviceCatchesUp(t *testing.T) {
	for _, seed := range seeds(t) {
		seed := seed
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			sc := generate(rng, streamSize())
			cmds := sc.order(rand.New(rand.NewSource(seed)))
			s := openStoreAt(t, filepath.Join(t.TempDir(), "devices.db"))

			devices := []*device{newDevice("web"), newDevice("phone"), newDevice("agent")}
			// The agent goes offline at a random point and stays offline.
			offlineFrom := rng.Intn(len(cmds))

			for i, c := range cmds {
				batch := []Command{c}
				if rng.Intn(4) == 0 {
					batch = append(batch, c) // the same command twice in one call
				}
				if _, _, err := s.Apply(batch); err != nil {
					t.Fatalf("seed %d: Apply failed at %d: %v", seed, i, err)
				}
				if rng.Intn(4) == 0 { // a lost response, so the client sends again
					if _, _, err := s.Apply(batch); err != nil {
						t.Fatalf("seed %d: the resend failed at %d: %v", seed, i, err)
					}
				}
				for d, dev := range devices {
					if d == 2 && i >= offlineFrom {
						continue // the agent is offline
					}
					if rng.Intn(3) == 0 {
						dev.pull(t, s)
					}
				}
			}

			// Everyone comes back, which is what a foreground event does.
			for _, dev := range devices {
				dev.pull(t, s)
			}
			fresh := newDevice("fresh")
			fresh.pull(t, s)

			want := snapshot(t, s)
			for _, dev := range append(devices, fresh) {
				if got := dev.state(); got != want {
					t.Fatalf("seed %d: the device %q did not catch up (offline from command %d).\n%s",
						seed, dev.name, offlineFrom, firstDifference(want, got))
				}
			}
			checkInvariants(t, s, fmt.Sprintf("seed %d", seed))
		})
	}
}

// --- a device that holds every table ----------------------------------------

// device is a simulated client. It holds the rows it knows and the version it
// last saw, which is what the web app and the Android app both do.
type device struct {
	name     string
	version  int64
	projects map[string]Project
	labels   map[string]Label
	tasks    map[string]Task
}

func newDevice(name string) *device {
	return &device{name: name, projects: map[string]Project{}, labels: map[string]Label{}, tasks: map[string]Task{}}
}

func (d *device) pull(t *testing.T, s *Store) {
	t.Helper()
	delta, err := s.Pull(d.version)
	if err != nil {
		t.Fatalf("the device %q could not pull: %v", d.name, err)
	}
	for _, p := range delta.Projects {
		d.projects[p.ID] = p
	}
	for _, l := range delta.Labels {
		d.labels[l.ID] = l
	}
	for _, task := range delta.Tasks {
		d.tasks[task.ID] = task
	}
	d.version = delta.Version
}

// state renders what the device believes, in the same shape as a snapshot of
// the store, so the two compare directly.
func (d *device) state() string {
	out := Delta{}
	for _, p := range d.projects {
		out.Projects = append(out.Projects, p)
	}
	for _, l := range d.labels {
		out.Labels = append(out.Labels, l)
	}
	for _, task := range d.tasks {
		out.Tasks = append(out.Tasks, task)
	}
	return renderDelta(out)
}

// --- the structural invariants ----------------------------------------------

// checkInvariants runs every check that must hold whatever the order of the
// commands. A failure names where it came from, because the same helper runs
// from several tests.
func checkInvariants(t *testing.T, s *Store, where string) {
	t.Helper()
	checkRowVersionIsTheLatestChange(t, s, where)
	checkEveryAcceptedCommandWroteToTheChangeLog(t, s, where)
	checkTheChangeLogHasNoPhantomRow(t, s, where)
	checkOneSearchRowPerTask(t, s, where)
}

// checkRowVersionIsTheLatestChange is the no gap property, stated on the
// server. A client pulls with `version > since`, so the version column of a
// row must be the highest change_log version that names it. If a write path
// forgets to bump, or bumps and then writes an older number, a client that
// pulls from an earlier version never sees the row again and the edit is lost
// for that client alone, which is the hardest kind of bug to find in the wild.
func checkRowVersionIsTheLatestChange(t *testing.T, s *Store, where string) {
	t.Helper()
	for _, table := range []string{"project", "label", "task"} {
		rows, err := s.db.Query(`SELECT r.id, r.version,
			(SELECT max(c.version) FROM change_log c WHERE c.tbl = ? AND c.row_id = r.id)
			FROM `+table+` r`, table)
		if err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		type bad struct {
			id           string
			held, latest int64
			latestIsNull bool
		}
		var problems []bad
		for rows.Next() {
			var id string
			var held int64
			var latest *int64
			if err := rows.Scan(&id, &held, &latest); err != nil {
				rows.Close()
				t.Fatalf("%s: %v", where, err)
			}
			switch {
			case latest == nil:
				problems = append(problems, bad{id, held, 0, true})
			case *latest != held:
				problems = append(problems, bad{id, held, *latest, false})
			}
		}
		err = rows.Err()
		rows.Close() // the pool holds one connection, so free it before the next query
		if err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		for _, p := range problems {
			if p.latestIsNull {
				t.Errorf("%s: the %s row %s is at version %d and the change log never names it, "+
					"so a client that pulls from an earlier version never sees it",
					where, table, p.id, p.held)
				continue
			}
			t.Errorf("%s: the %s row %s holds version %d and its last change was %d, "+
				"so a pull from between the two misses the row", where, table, p.id, p.held, p.latest)
		}
	}
}

// checkEveryAcceptedCommandWroteToTheChangeLog is the other half of no lost
// edit. applied_command records the version after the command ran, so two
// accepted commands with one version means one of them changed nothing while
// the store answered OK. The client then drops it from the outbox and the edit
// is gone for good.
func checkEveryAcceptedCommandWroteToTheChangeLog(t *testing.T, s *Store, where string) {
	t.Helper()
	var total, distinct int
	if err := s.db.QueryRow(`SELECT count(*), count(DISTINCT version) FROM applied_command`).
		Scan(&total, &distinct); err != nil {
		t.Fatalf("%s: %v", where, err)
	}
	if total != distinct {
		t.Errorf("%s: %d commands were accepted and they share %d versions, so %d of them "+
			"changed nothing while the store answered OK", where, total, distinct, total-distinct)
	}
}

// checkTheChangeLogHasNoPhantomRow proves that a version always carries a row.
// A phantom entry wakes every client through SSE for nothing, and it makes the
// change log disagree with the tables it is supposed to describe.
func checkTheChangeLogHasNoPhantomRow(t *testing.T, s *Store, where string) {
	t.Helper()
	for _, table := range []string{"project", "label", "task"} {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM change_log c WHERE c.tbl = ?
			AND NOT EXISTS (SELECT 1 FROM `+table+` r WHERE r.id = c.row_id)`, table).Scan(&n); err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		if n > 0 {
			t.Errorf("%s: the change log holds %d entries for a %s row that does not exist",
				where, n, table)
		}
	}
}

// checkOneSearchRowPerTask guards the full text index, which the store writes
// by hand on every task change. Two rows for one task make search answer twice
// and no row makes it answer never.
func checkOneSearchRowPerTask(t *testing.T, s *Store, where string) {
	t.Helper()
	var tasks, indexed, distinct int
	if err := s.db.QueryRow(`SELECT count(*) FROM task`).Scan(&tasks); err != nil {
		t.Fatalf("%s: %v", where, err)
	}
	if err := s.db.QueryRow(`SELECT count(*), count(DISTINCT task_id) FROM task_fts`).
		Scan(&indexed, &distinct); err != nil {
		t.Fatalf("%s: %v", where, err)
	}
	if indexed != distinct {
		t.Errorf("%s: the search index holds %d rows for %d tasks, so a task is indexed twice",
			where, indexed, distinct)
	}
	if distinct != tasks {
		t.Errorf("%s: %d tasks and %d of them in the search index", where, tasks, distinct)
	}
}

// --- small helpers ----------------------------------------------------------

func applyInBatches(t *testing.T, s *Store, cmds []Command, rng *rand.Rand, seed int64) {
	t.Helper()
	for from := 0; from < len(cmds); {
		to := from + 1 + rng.Intn(8)
		if to > len(cmds) {
			to = len(cmds)
		}
		if _, res, err := s.Apply(cmds[from:to]); err != nil {
			t.Fatalf("seed %d: Apply failed at %d: %v", seed, from, err)
		} else {
			for _, r := range res {
				if !r.OK {
					t.Fatalf("seed %d: the command %s failed: %s. Every generated command must apply",
						seed, r.UUID, r.Error)
				}
			}
		}
		from = to
	}
}

func version(t *testing.T, s *Store) int64 {
	t.Helper()
	v, err := s.Version()
	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}
	return v
}

func acceptedCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM applied_command`).Scan(&n); err != nil {
		t.Fatalf("counting applied_command failed: %v", err)
	}
	return n
}

func countSamePlace(a, b []Command) int {
	n := 0
	for i := range a {
		if i < len(b) && a[i].UUID == b[i].UUID {
			n++
		}
	}
	return n
}

// firstDifference shrinks a failure by hand: Go has no shrinker, so the
// message names the first line that differs instead of printing two states of
// a few hundred rows. The line names the row, so the seed plus the line is
// enough to find the two commands that disagreed.
func firstDifference(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			return fmt.Sprintf("line %d of %d differs:\n  one: %s\n  two: %s", i+1, max(len(wl), len(gl)), w, g)
		}
	}
	return "the two states are equal, so the comparison itself is broken"
}
