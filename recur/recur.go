// SPDX-License-Identifier: Apache-2.0

// Package recur computes the next date of a repeating task.
//
// The wire format is an RFC 5545 RRULE string. The clients parse natural
// language ("every monday") and send the rule, so this package holds the only
// recurrence engine on the server.
package recur

import (
	"fmt"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

const dateLayout = "2006-01-02"

// Next returns the next date for a rule, as YYYY-MM-DD.
//
// base is the current due date. today is the date of completion. When
// fromCompletion is true, the count starts at the completion date, so a chore
// done late moves forward from today instead of piling up.
// An empty return value means that the rule has no further date.
func Next(rule, base, today string, fromCompletion bool) (string, error) {
	start := base
	if fromCompletion || start == "" {
		start = today
	}
	startTime, err := time.Parse(dateLayout, start)
	if err != nil {
		return "", fmt.Errorf("bad base date %q: %w", start, err)
	}
	set, err := parse(rule, startTime)
	if err != nil {
		return "", err
	}
	// The next date is the first occurrence strictly after both the current due
	// date and the day of completion. A chore two months overdue therefore
	// moves to its next real slot, not to the slot it missed.
	after := startTime
	if todayTime, err := time.Parse(dateLayout, today); err == nil && todayTime.After(after) {
		after = todayTime
	}
	next := set.After(after, false)
	if next.IsZero() {
		return "", nil
	}
	return next.Format(dateLayout), nil
}

// Valid reports whether a rule parses.
func Valid(rule string) error {
	_, err := parse(rule, time.Now())
	return err
}

func parse(rule string, start time.Time) (*rrule.Set, error) {
	text := strings.TrimSpace(rule)
	if text == "" {
		return nil, fmt.Errorf("empty rule")
	}
	if !strings.HasPrefix(strings.ToUpper(text), "RRULE:") && !strings.Contains(text, "\n") {
		text = "RRULE:" + text
	}
	set, err := rrule.StrToRRuleSet(text)
	if err != nil {
		return nil, fmt.Errorf("bad rrule %q: %w", rule, err)
	}
	set.DTStart(start)
	return set, nil
}
