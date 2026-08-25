// SPDX-License-Identifier: Apache-2.0

package id

import (
	"sort"
	"strings"
	"testing"
	"time"
)

func TestShapeAndLength(t *testing.T) {
	got := New("t")
	if !strings.HasPrefix(got, "t_") {
		t.Fatalf("%q lacks the prefix", got)
	}
	body := strings.TrimPrefix(got, "t_")
	if len(body) != 15 {
		t.Fatalf("%q has %d characters after the prefix, want 15", got, len(body))
	}
	for _, r := range body {
		if !strings.ContainsRune(alphabet, r) {
			t.Fatalf("%q holds the character %q, which is not in the alphabet", got, r)
		}
	}
	// A short id is the reason the wire format stays cheap. A UUID is 36
	// characters, so guard the size.
	if len(got) > 18 {
		t.Fatalf("%q is %d characters: an id must stay short", got, len(got))
	}
}

// A bulk import creates thousands of rows inside one millisecond, so this is
// the case that matters, not the leisurely one.
func TestUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200000; i++ {
		v := New("t")
		if seen[v] {
			t.Fatalf("id %q repeated after %d draws", v, i)
		}
		seen[v] = true
	}
}

// The time prefix must sort in creation order, so a database index stays dense
// and a list keeps a stable order without a second column.
func TestSortsByTime(t *testing.T) {
	first := New("t")
	time.Sleep(2 * time.Millisecond)
	second := New("t")
	time.Sleep(2 * time.Millisecond)
	third := New("t")
	got := []string{third, first, second}
	sort.Strings(got)
	if got[0] != first || got[1] != second || got[2] != third {
		t.Fatalf("sorted order is %v, want %v", got, []string{first, second, third})
	}
}

func TestPrefixIsFree(t *testing.T) {
	for _, p := range []string{"t", "p", "l"} {
		if !strings.HasPrefix(New(p), p+"_") {
			t.Errorf("prefix %q did not survive", p)
		}
	}
}
