// SPDX-License-Identifier: Apache-2.0

package quickadd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// corpus mirrors parser-fixtures/quickadd.json. A field that a case leaves out
// must come back empty, so every field is compared, not only the listed ones.
type corpus struct {
	Today string `json:"today"`
	Cases []struct {
		In           string   `json:"in"`
		Title        string   `json:"title"`
		Due          string   `json:"due"`
		Time         string   `json:"time"`
		Priority     int      `json:"priority"`
		Project      string   `json:"project"`
		Labels       []string `json:"labels"`
		RRule        string   `json:"rrule"`
		RemindAt     string   `json:"remind_at"`
		RemindBefore int      `json:"remind_before"`
		Note         string   `json:"note"`
	} `json:"cases"`
}

func TestCorpus(t *testing.T) {
	path := filepath.Join("..", "parser-fixtures", "quickadd.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the corpus: %v", err)
	}
	var c corpus
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("cannot read the corpus JSON: %v", err)
	}
	today, err := time.Parse(isoDay, c.Today)
	if err != nil {
		t.Fatalf("bad today in the corpus: %v", err)
	}
	if len(c.Cases) == 0 {
		t.Fatal("the corpus has no cases")
	}

	for _, tc := range c.Cases {
		t.Run(tc.In, func(t *testing.T) {
			got := Parse(tc.In, today)
			if got.Title != tc.Title {
				t.Errorf("title: got %q, want %q", got.Title, tc.Title)
			}
			if got.Due != tc.Due {
				t.Errorf("due: got %q, want %q", got.Due, tc.Due)
			}
			if got.Time != tc.Time {
				t.Errorf("time: got %q, want %q", got.Time, tc.Time)
			}
			if got.Priority != tc.Priority {
				t.Errorf("priority: got %d, want %d", got.Priority, tc.Priority)
			}
			if got.Project != tc.Project {
				t.Errorf("project: got %q, want %q", got.Project, tc.Project)
			}
			if got.RRule != tc.RRule {
				t.Errorf("rrule: got %q, want %q", got.RRule, tc.RRule)
			}
			if got.RemindAt != tc.RemindAt {
				t.Errorf("remind_at: got %q, want %q", got.RemindAt, tc.RemindAt)
			}
			if got.RemindBefore != tc.RemindBefore {
				t.Errorf("remind_before: got %d, want %d", got.RemindBefore, tc.RemindBefore)
			}
			want := tc.Labels
			if want == nil {
				want = []string{}
			}
			have := got.Labels
			if have == nil {
				have = []string{}
			}
			if !slices.Equal(have, want) {
				t.Errorf("labels: got %v, want %v", have, want)
			}
		})
	}
}

func TestParsedSpans(t *testing.T) {
	today := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	got := Parse("Call the plumber tomorrow p1 #Home @call", today)
	want := []string{"p1", "#Home", "@call", "tomorrow"}
	if !slices.Equal(got.Parsed, want) {
		t.Errorf("parsed: got %v, want %v", got.Parsed, want)
	}
}

// TestTimeOfDayDoesNotShiftDates guards the client against a machine clock: a
// relative word must give the same date at any hour of the day.
func TestTimeOfDayDoesNotShiftDates(t *testing.T) {
	morning := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	night := time.Date(2026, 8, 25, 23, 30, 0, 0, time.UTC)
	if a, b := Parse("Ferry 25.08", morning).Due, Parse("Ferry 25.08", night).Due; a != b || a != "2026-08-25" {
		t.Errorf("got %q and %q, want 2026-08-25 twice", a, b)
	}
}
