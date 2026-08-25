// SPDX-License-Identifier: Apache-2.0

package recur

import "testing"

func TestNext(t *testing.T) {
	// 2026-08-25 is a Tuesday. Every case states the rule, the current due
	// date, the day of completion, and the date the task must move to.
	cases := []struct {
		name           string
		rule           string
		due            string
		today          string
		fromCompletion bool
		want           string
	}{
		{"daily, done on time", "FREQ=DAILY", "2026-08-25", "2026-08-25", false, "2026-08-26"},
		{"daily, done early", "FREQ=DAILY", "2026-08-27", "2026-08-25", false, "2026-08-28"},
		{"weekly on Tuesday", "FREQ=WEEKLY;BYDAY=TU", "2026-08-25", "2026-08-25", false, "2026-09-01"},
		{"weekly, two months late", "FREQ=WEEKLY;BYDAY=TU", "2026-06-02", "2026-08-25", false, "2026-09-01"},
		{"every second week", "FREQ=WEEKLY;INTERVAL=2", "2026-08-25", "2026-08-25", false, "2026-09-08"},
		{"weekdays only", "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", "2026-08-28", "2026-08-28", false, "2026-08-31"},
		{"monthly", "FREQ=MONTHLY", "2026-08-23", "2026-08-25", false, "2026-09-23"},
		{"monthly on the last day", "FREQ=MONTHLY;BYMONTHDAY=-1", "2026-08-31", "2026-08-31", false, "2026-09-30"},
		{"yearly", "FREQ=YEARLY", "2026-08-25", "2026-08-25", false, "2027-08-25"},
		{"from completion, three days", "FREQ=DAILY;INTERVAL=3", "2026-06-02", "2026-08-25", true, "2026-08-28"},
		{"from completion ignores the old date", "FREQ=WEEKLY", "2026-01-01", "2026-08-25", true, "2026-09-01"},
		{"no due date falls back to today", "FREQ=DAILY", "", "2026-08-25", false, "2026-08-26"},
		{"with the RRULE prefix", "RRULE:FREQ=DAILY", "2026-08-25", "2026-08-25", false, "2026-08-26"},
		{"a rule that ends returns nothing", "FREQ=DAILY;COUNT=1", "2026-08-25", "2026-08-25", false, ""},
		{"leap day, yearly", "FREQ=YEARLY", "2028-02-29", "2028-02-29", false, "2032-02-29"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Next(c.rule, c.due, c.today, c.fromCompletion)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestNextRejectsBadInput(t *testing.T) {
	cases := []struct{ name, rule, due string }{
		{"empty rule", "", "2026-08-25"},
		{"nonsense rule", "every other tuesday", "2026-08-25"},
		{"bad frequency", "FREQ=FORTNIGHTLY", "2026-08-25"},
		{"bad date", "FREQ=DAILY", "25.08.2026"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Next(c.rule, c.due, "2026-08-25", false); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestValid(t *testing.T) {
	for _, ok := range []string{"FREQ=DAILY", "RRULE:FREQ=WEEKLY;BYDAY=MO,WE", "FREQ=MONTHLY;BYMONTHDAY=1"} {
		if err := Valid(ok); err != nil {
			t.Errorf("%q must be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "every week", "FREQ="} {
		if err := Valid(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
