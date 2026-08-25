// SPDX-License-Identifier: AGPL-3.0-or-later

package todoist

import "testing"

func TestConvertRecurrence(t *testing.T) {
	cases := []struct {
		in            string
		rule          string
		afterComplete bool
	}{
		// The abbreviated keyword. A real account of 250 tasks had 8 rules that
		// did not convert, and 7 of them differed from a supported form by this
		// word alone.
		{in: "ev year", rule: "FREQ=YEARLY"},
		{in: "ev mon", rule: "FREQ=WEEKLY;BYDAY=MO"},
		{in: "ev 2nd sun", rule: "FREQ=MONTHLY;BYDAY=2SU"},
		{in: "ev 4th sat", rule: "FREQ=MONTHLY;BYDAY=4SA"},
		{in: "ev 3 months", rule: "FREQ=MONTHLY;INTERVAL=3"},
		{in: "ev workday", rule: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
		{in: "evry day", rule: "FREQ=DAILY"},
		// An ordinal over the working week, not over one weekday name.
		{in: "ev last workday", rule: "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1"},
		{in: "every first workday", rule: "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=1"},
		{in: "every 2nd weekday of the month", rule: "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=2"},
		{in: "every day", rule: "FREQ=DAILY"},
		{in: "daily", rule: "FREQ=DAILY"},
		{in: "Every Day", rule: "FREQ=DAILY"},
		{in: "every day at 10:00", rule: "FREQ=DAILY"},
		{in: "every 2 days", rule: "FREQ=DAILY;INTERVAL=2"},
		{in: "every 1 day", rule: "FREQ=DAILY"},
		{in: "every other day", rule: "FREQ=DAILY;INTERVAL=2"},
		{in: "every week", rule: "FREQ=WEEKLY"},
		{in: "weekly", rule: "FREQ=WEEKLY"},
		{in: "every monday", rule: "FREQ=WEEKLY;BYDAY=MO"},
		{in: "every mon", rule: "FREQ=WEEKLY;BYDAY=MO"},
		{in: "every mon, fri", rule: "FREQ=WEEKLY;BYDAY=MO,FR"},
		{in: "every wed and sat", rule: "FREQ=WEEKLY;BYDAY=WE,SA"},
		{in: "every week on tuesday", rule: "FREQ=WEEKLY;BYDAY=TU"},
		{in: "every 2 weeks", rule: "FREQ=WEEKLY;INTERVAL=2"},
		{in: "every 2 weeks on monday", rule: "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO"},
		{in: "every other week", rule: "FREQ=WEEKLY;INTERVAL=2"},
		{in: "every month", rule: "FREQ=MONTHLY"},
		{in: "monthly", rule: "FREQ=MONTHLY"},
		{in: "every 3 months", rule: "FREQ=MONTHLY;INTERVAL=3"},
		{in: "every year", rule: "FREQ=YEARLY"},
		{in: "every 2 years", rule: "FREQ=YEARLY;INTERVAL=2"},
		{in: "every weekday", rule: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
		{in: "every workday", rule: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
		{in: "every work day", rule: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
		{in: "every weekend", rule: "FREQ=WEEKLY;BYDAY=SA,SU"},
		{in: "every last day", rule: "FREQ=MONTHLY;BYMONTHDAY=-1"},
		{in: "every last day of the month", rule: "FREQ=MONTHLY;BYMONTHDAY=-1"},
		{in: "every 2nd tuesday", rule: "FREQ=MONTHLY;BYDAY=2TU"},
		{in: "every last friday", rule: "FREQ=MONTHLY;BYDAY=-1FR"},
		{in: "every 15th", rule: "FREQ=MONTHLY;BYMONTHDAY=15"},
		// Todoist writes the "!" form when the next date counts from the day of
		// completion.
		{in: "every! 2 weeks", rule: "FREQ=WEEKLY;INTERVAL=2", afterComplete: true},
		{in: "every! day", rule: "FREQ=DAILY", afterComplete: true},
	}
	for _, c := range cases {
		got, ok := ConvertRecurrence(c.in)
		if !ok {
			t.Errorf("ConvertRecurrence(%q) failed, want %q", c.in, c.rule)
			continue
		}
		if got.RRule != c.rule {
			t.Errorf("ConvertRecurrence(%q) = %q, want %q", c.in, got.RRule, c.rule)
		}
		if got.FromCompletion != c.afterComplete {
			t.Errorf("ConvertRecurrence(%q) from completion = %v, want %v", c.in, got.FromCompletion, c.afterComplete)
		}
	}
}

// TestConvertRecurrenceFails lists the strings that our model cannot hold. The
// importer keeps such a task and writes the original words into the
// description, so a clean failure matters as much as a clean conversion.
func TestConvertRecurrenceFails(t *testing.T) {
	bad := []string{
		"every jan 15",
		"every day starting 1 sep",
		"every mon ending 1 dec",
		"every day for 3 weeks",
		"every 3 hours",
		"every 30 minutes",
		"every 4th thursday of november",
		"tomorrow",
		"",
		"   ",
		"every",
		"every 0 days",
		"every 40th",
	}
	for _, in := range bad {
		if got, ok := ConvertRecurrence(in); ok {
			t.Errorf("ConvertRecurrence(%q) = %q, want a clean failure", in, got.RRule)
		}
	}
}

func TestMapPriority(t *testing.T) {
	// Todoist sends 4 for the p1 that the user sees. Our store keeps the number
	// that the user sees, so the two scales run in opposite directions.
	cases := map[int]int{
		4:  1, // p1, urgent
		3:  2, // p2
		2:  3, // p3
		1:  4, // p4, no priority
		0:  4, // the field was absent
		9:  1, // out of range, clamped
		-3: 4,
	}
	for api, want := range cases {
		if got := MapPriority(api); got != want {
			t.Errorf("MapPriority(%d) = %d, want %d", api, got, want)
		}
	}
}

func TestSplitDue(t *testing.T) {
	cases := []struct {
		due        *Due
		date, time string
	}{
		{due: nil},
		{due: &Due{Date: "2026-09-01"}, date: "2026-09-01"},
		{due: &Due{Date: "2026-09-01T09:30:00"}, date: "2026-09-01", time: "09:30"},
		{due: &Due{Date: "2026-08-30T13:00:00Z"}, date: "2026-08-30", time: "13:00"},
	}
	for _, c := range cases {
		date, clock := splitDue(c.due)
		if date != c.date || clock != c.time {
			t.Errorf("splitDue(%+v) = %q %q, want %q %q", c.due, date, clock, c.date, c.time)
		}
	}
}
