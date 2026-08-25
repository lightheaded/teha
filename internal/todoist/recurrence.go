// SPDX-License-Identifier: AGPL-3.0-or-later

package todoist

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lightheaded/teha/recur"
)

// Recurrence is a converted repeat rule.
type Recurrence struct {
	// RRule is an RFC 5545 rule without the "RRULE:" prefix, the form that the
	// store keeps in the rrule column.
	RRule string
	// FromCompletion is true for a Todoist rule written with "every!", which
	// counts the next date from the day of completion.
	FromCompletion bool
}

var (
	multiSpace = regexp.MustCompile(`\s+`)
	// timeClause drops "at 10am" and the like. Our model keeps the clock time
	// in due_time, so the rule needs the date part only.
	timeClause = regexp.MustCompile(`\s+at\s+\S.*$`)

	reEveryDay     = regexp.MustCompile(`^(daily|every day)$`)
	reEveryWeek    = regexp.MustCompile(`^(weekly|every week)$`)
	reEveryMonth   = regexp.MustCompile(`^(monthly|every month)$`)
	reEveryYear    = regexp.MustCompile(`^(yearly|annually|every year)$`)
	reWeekdays     = regexp.MustCompile(`^every (week ?day|work ?day)s?$`)
	reWeekend      = regexp.MustCompile(`^every weekend( day)?s?$`)
	reLastDay      = regexp.MustCompile(`^every last day( of (the )?month)?$`)
	reEveryNDays   = regexp.MustCompile(`^every (other|\d+) days?$`)
	reEveryNWeeks  = regexp.MustCompile(`^every (other|\d+) weeks?(?: on (.+))?$`)
	reEveryNMonths = regexp.MustCompile(`^every (other|\d+) months?$`)
	reEveryNYears  = regexp.MustCompile(`^every (other|\d+) years?$`)
	reNthWeekday   = regexp.MustCompile(`^every (first|second|third|fourth|fifth|last|1st|2nd|3rd|4th|5th) ([a-z]+)( of (the )?month)?$`)
	// "every last workday" is an ordinal over the working week, not over one
	// weekday name, so BYDAY carries five days and BYSETPOS picks one of them.
	reNthWorkday = regexp.MustCompile(`^every (first|second|third|fourth|fifth|last|1st|2nd|3rd|4th|5th) (week ?day|work ?day)s?( of (the )?month)?$`)
	// Todoist abbreviates the leading keyword in the app and in its exports.
	// "ev year" and "every year" are the same rule, so normalise before every
	// pattern below sees the text. Found in a real account: 7 of its 8
	// unconvertible rules differed from a supported form by this word alone.
	reEveryWord  = regexp.MustCompile(`^(ev|evry|every)\b`)
	reMonthDay   = regexp.MustCompile(`^every (\d{1,2})(?:st|nd|rd|th)( of (the )?month)?$`)
	reWeekOnList = regexp.MustCompile(`^every week on (.+)$`)
	reEveryList  = regexp.MustCompile(`^every (.+)$`)
)

// unsupportedClause names the words of a Todoist rule that our model cannot
// hold: a start date, an end date, or a count. The importer keeps such a task
// and writes the original words into the description.
var unsupportedClause = []string{"starting", "ending", "ends", "begins", "until", "for", "from", "hour", "hours", "minute", "minutes"}

var weekdays = map[string]string{
	"mon": "MO", "mo": "MO", "monday": "MO", "mondays": "MO",
	"tue": "TU", "tues": "TU", "tu": "TU", "tuesday": "TU", "tuesdays": "TU",
	"wed": "WE", "weds": "WE", "we": "WE", "wednesday": "WE", "wednesdays": "WE",
	"thu": "TH", "thur": "TH", "thurs": "TH", "th": "TH", "thursday": "TH", "thursdays": "TH",
	"fri": "FR", "fr": "FR", "friday": "FR", "fridays": "FR",
	"sat": "SA", "sa": "SA", "saturday": "SA", "saturdays": "SA",
	"sun": "SU", "su": "SU", "sunday": "SU", "sundays": "SU",
}

var ordinals = map[string]int{
	"first": 1, "1st": 1,
	"second": 2, "2nd": 2,
	"third": 3, "3rd": 3,
	"fourth": 4, "4th": 4,
	"fifth": 5, "5th": 5,
	"last": -1,
}

// ConvertRecurrence turns a Todoist due string such as "every 2 weeks" into an
// RRULE. The second return value is false when the words do not convert; the
// caller then keeps the task and records the original words.
func ConvertRecurrence(text string) (Recurrence, bool) {
	r, ok := convertRecurrence(text)
	if !ok {
		return Recurrence{}, false
	}
	// A rule that the store cannot read is worse than no rule at all, so the
	// output goes through the same parser that the server uses.
	if err := recur.Valid(r.RRule); err != nil {
		return Recurrence{}, false
	}
	return r, true
}

func convertRecurrence(text string) (Recurrence, bool) {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return Recurrence{}, false
	}
	// Todoist writes "every! 2 weeks" when the next date counts from the day of
	// completion.
	fromCompletion := strings.Contains(s, "!")
	s = strings.ReplaceAll(s, "!", "")
	s = strings.TrimSpace(multiSpace.ReplaceAllString(s, " "))
	s = timeClause.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = reEveryWord.ReplaceAllString(s, "every")

	for _, word := range strings.Fields(s) {
		for _, bad := range unsupportedClause {
			if word == bad {
				return Recurrence{}, false
			}
		}
	}

	out := func(rule string) (Recurrence, bool) {
		return Recurrence{RRule: rule, FromCompletion: fromCompletion}, true
	}

	switch {
	case reEveryDay.MatchString(s):
		return out("FREQ=DAILY")
	case reEveryWeek.MatchString(s):
		return out("FREQ=WEEKLY")
	case reEveryMonth.MatchString(s):
		return out("FREQ=MONTHLY")
	case reEveryYear.MatchString(s):
		return out("FREQ=YEARLY")
	case reWeekdays.MatchString(s):
		return out("FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR")
	case reWeekend.MatchString(s):
		return out("FREQ=WEEKLY;BYDAY=SA,SU")
	case reLastDay.MatchString(s):
		return out("FREQ=MONTHLY;BYMONTHDAY=-1")
	}

	if m := reEveryNDays.FindStringSubmatch(s); m != nil {
		n, ok := interval(m[1])
		if !ok {
			return Recurrence{}, false
		}
		return out(withInterval("FREQ=DAILY", n))
	}
	if m := reEveryNWeeks.FindStringSubmatch(s); m != nil {
		n, ok := interval(m[1])
		if !ok {
			return Recurrence{}, false
		}
		rule := withInterval("FREQ=WEEKLY", n)
		if m[2] != "" {
			days, ok := weekdayList(m[2])
			if !ok {
				return Recurrence{}, false
			}
			rule += ";BYDAY=" + days
		}
		return out(rule)
	}
	if m := reEveryNMonths.FindStringSubmatch(s); m != nil {
		n, ok := interval(m[1])
		if !ok {
			return Recurrence{}, false
		}
		return out(withInterval("FREQ=MONTHLY", n))
	}
	if m := reEveryNYears.FindStringSubmatch(s); m != nil {
		n, ok := interval(m[1])
		if !ok {
			return Recurrence{}, false
		}
		return out(withInterval("FREQ=YEARLY", n))
	}
	if m := reNthWorkday.FindStringSubmatch(s); m != nil {
		n, ok := ordinals[m[1]]
		if !ok {
			return Recurrence{}, false
		}
		return Recurrence{
			RRule:          fmt.Sprintf("FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=%d", n),
			FromCompletion: fromCompletion,
		}, true
	}

	if m := reNthWeekday.FindStringSubmatch(s); m != nil {
		nth, known := ordinals[m[1]]
		day, isDay := weekdays[m[2]]
		if known && isDay {
			return out(fmt.Sprintf("FREQ=MONTHLY;BYDAY=%d%s", nth, day))
		}
		return Recurrence{}, false
	}
	if m := reMonthDay.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > 31 {
			return Recurrence{}, false
		}
		return out(fmt.Sprintf("FREQ=MONTHLY;BYMONTHDAY=%d", n))
	}
	if m := reWeekOnList.FindStringSubmatch(s); m != nil {
		days, ok := weekdayList(m[1])
		if !ok {
			return Recurrence{}, false
		}
		return out("FREQ=WEEKLY;BYDAY=" + days)
	}
	if m := reEveryList.FindStringSubmatch(s); m != nil {
		days, ok := weekdayList(m[1])
		if !ok {
			return Recurrence{}, false
		}
		return out("FREQ=WEEKLY;BYDAY=" + days)
	}
	return Recurrence{}, false
}

// interval reads "other" or a number. Zero is not a repeat.
func interval(word string) (int, bool) {
	if word == "other" {
		return 2, true
	}
	n, err := strconv.Atoi(word)
	if err != nil || n < 1 || n > 999 {
		return 0, false
	}
	return n, true
}

func withInterval(rule string, n int) string {
	if n <= 1 {
		return rule
	}
	return fmt.Sprintf("%s;INTERVAL=%d", rule, n)
}

// weekdayList reads "mon, wed and fri" and returns "MO,WE,FR". Every word must
// be a weekday, so "jan 15" fails here and lands in the description.
func weekdayList(text string) (string, bool) {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == '/' || r == ' ' || r == '\t'
	})
	var days []string
	seen := map[string]bool{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" || f == "and" || f == "the" || f == "on" {
			continue
		}
		day, ok := weekdays[f]
		if !ok {
			return "", false
		}
		if seen[day] {
			continue
		}
		seen[day] = true
		days = append(days, day)
	}
	if len(days) == 0 {
		return "", false
	}
	return strings.Join(days, ","), true
}
