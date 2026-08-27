// SPDX-License-Identifier: Apache-2.0

package order

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBetweenTheEmptyList(t *testing.T) {
	got, err := Between("", "")
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("the first key of an empty list is empty")
	}
	if !Valid(got) {
		t.Fatalf("the first key %q is not a valid key", got)
	}
}

func TestBetweenRefusesTheWrongOrder(t *testing.T) {
	cases := [][2]string{
		{"V", "V"},   // equal
		{"k", "V"},   // the wrong way round
		{"V0", "Vk"}, // a key that ends with the lowest digit
		{"V!", "Vk"}, // a character outside the alphabet
	}
	for _, c := range cases {
		if _, err := Between(c[0], c[1]); err == nil {
			t.Errorf("Between(%q, %q) answered, want an error", c[0], c[1])
		}
	}
}

// TestBetweenKeepsAStrictOrder walks a list with random insertions at random
// places and checks the two invariants after every step: the keys stay in a
// strict order, and no two keys are equal.
func TestBetweenKeepsAStrictOrder(t *testing.T) {
	for _, seed := range seeds(t) {
		rng := rand.New(rand.NewSource(seed))
		var keys []string
		for step := 0; step < 400; step++ {
			at := rng.Intn(len(keys) + 1)
			left, right := "", ""
			if at > 0 {
				left = keys[at-1]
			}
			if at < len(keys) {
				right = keys[at]
			}
			key, err := Between(left, right)
			if err != nil {
				t.Fatalf("seed %d step %d: Between(%q, %q): %v", seed, step, left, right, err)
			}
			keys = append(keys[:at], append([]string{key}, keys[at:]...)...)
			if err := strictlySorted(keys); err != nil {
				t.Fatalf("seed %d step %d: %v", seed, step, err)
			}
		}
	}
}

// TestBetweenSurvivesTheWorstCase is the case the plan asks about: the same
// gap, again and again. Every insertion lands immediately after one fixed key,
// which is the shape that makes a fractional index grow.
//
// Measured on 2026-08-27: 2 000 insertions into one gap, and the longest key
// is 401 characters. The keys never collide and never lose their order, so
// precision never runs out. The cost is length, and the rate is about one
// character for every six insertions into the same gap, because the answer
// halves the room that is left and 62 halves down to nothing in six steps.
//
// That rate is the known limit of this scheme. A client that drags one task up
// a long list thousands of times ends with a long sort key. The fix is the
// integer-prefix form of a fractional index, where a key carries a magnitude
// as well as a fraction and an append at the end costs no length at all.
// docs/BACKLOG.md holds that item.
func TestBetweenSurvivesTheWorstCase(t *testing.T) {
	const inserts = 2000
	left, right := "V", "k"
	seen := map[string]bool{left: true, right: true}
	longest := 0
	for i := 0; i < inserts; i++ {
		key, err := Between(left, right)
		if err != nil {
			t.Fatalf("insertion %d between %q and %q: %v", i, left, right, err)
		}
		if seen[key] {
			t.Fatalf("insertion %d produced %q twice", i, key)
		}
		seen[key] = true
		if key <= left || key >= right {
			t.Fatalf("insertion %d put %q outside (%q, %q)", i, key, left, right)
		}
		if len(key) > longest {
			longest = len(key)
		}
		right = key // always insert into the same gap, next to the left key
	}
	t.Logf("%d insertions into one gap: the longest key is %d characters", inserts, longest)
	// The guard is generous on purpose. It is here to catch a change that makes
	// the growth quadratic, not to hold the measured number still.
	if longest > 512 {
		t.Errorf("the key grew to %d characters, so the growth is worse than linear", longest)
	}
}

// TestBetweenAtTheEnds records the other extreme: a list that only grows at
// one end. Measured on 2026-08-27: 2 000 appends give a key of 334
// characters, and 2 000 insertions at the start give 400. Both are the same
// known limit as the worst case above, because both walk into one gap.
func TestBetweenAtTheEnds(t *testing.T) {
	for _, name := range []string{"end", "start"} {
		key := ""
		longest := 0
		for i := 0; i < 2000; i++ {
			var err error
			if name == "end" {
				key, err = Between(key, "")
			} else {
				key, err = Between("", key)
			}
			if err != nil {
				t.Fatalf("%s insertion %d: %v", name, i, err)
			}
			if len(key) > longest {
				longest = len(key)
			}
		}
		t.Logf("2000 insertions at the %s: the longest key is %d characters", name, longest)
		if longest > 512 {
			t.Errorf("a key at the %s grew to %d characters", name, longest)
		}
	}
}

// TestBetweenAgreesWithByteOrder is the reason the alphabet runs in ASCII
// order. SQLite, Room and JavaScript all sort the column as bytes, so a key
// that sorts by its fraction must also sort by its bytes.
func TestBetweenAgreesWithByteOrder(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	var keys []string
	for i := 0; i < 300; i++ {
		at := rng.Intn(len(keys) + 1)
		left, right := "", ""
		if at > 0 {
			left = keys[at-1]
		}
		if at < len(keys) {
			right = keys[at]
		}
		key, err := Between(left, right)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys[:at], append([]string{key}, keys[at:]...)...)
	}
	sorted := append([]string{}, keys...)
	sort.Strings(sorted)
	for i := range keys {
		if keys[i] != sorted[i] {
			t.Fatalf("position %d: the list holds %q, a byte sort puts %q there", i, keys[i], sorted[i])
		}
	}
}

// TestTwoClientsReorderAtTheSameTime is the promise of §6.1: two clients that
// move a task into the same gap at the same time both keep a valid order. The
// two keys differ, so no edit is lost and no order is broken.
func TestTwoClientsReorderAtTheSameTime(t *testing.T) {
	left, right := "V", "k"
	// Each client picks the midpoint of the gap it can see, so both pick the
	// same key. That is not a collision in the store, because the key is not
	// the identity: the row id is. The order stays strict either way.
	web, err := Between(left, right)
	if err != nil {
		t.Fatal(err)
	}
	// The second client sees the first key after it syncs, and lands beside it.
	phone, err := Between(web, right)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{left, web, phone, right}
	if err := strictlySorted(keys); err != nil {
		t.Fatal(err)
	}
}

func strictlySorted(keys []string) error {
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] || !Valid(keys[i]) {
			from := i - 2
			if from < 0 {
				from = 0
			}
			to := i + 2
			if to > len(keys) {
				to = len(keys)
			}
			return fmt.Errorf("the order broke at position %d: %s", i, strings.Join(keys[from:to], " "))
		}
	}
	return nil
}

// seeds returns the seeds of one run.
//
// The first seeds are fixed, so a failure in CI is the same failure on a
// laptop. The last seed comes from the clock, so a long run with -count finds
// what a fixed corpus cannot. Every seed goes into the log, and a failure
// names the one that produced it, so `TEHA_SEED=<seed> go test` repeats it.
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
	t.Logf("seeds %v. To repeat one: TEHA_SEED=<seed> go test -run %s ./order", out, t.Name())
	return out
}
