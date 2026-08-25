// SPDX-License-Identifier: Apache-2.0

// Package quickadd turns one line of text into task fields.
//
// The corpus in parser-fixtures/quickadd.json is the contract. The web parser
// in internal/webui/assets/parse.js and this package must agree on every case,
// so a person gets the same result from the keyboard and from the browser. A
// change starts with a new fixture, never with new code here.
package quickadd

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Result holds the fields that one line produced. An empty string, an empty
// list or a zero priority means the line said nothing about that field.
type Result struct {
	Title    string
	Due      string // "2006-01-02"
	Time     string // "15:04"
	Priority int    // 1 to 4, or 0 when the line has no priority
	Project  string
	Labels   []string
	RRule    string
	// Parsed holds the pieces the parser took out of the line, in the order it
	// took them. A client shows them to explain what it understood.
	Parsed []string
}

// The patterns keep the shape of the web parser, so a reader compares the two
// files side by side. Go finds the same match as a backtracking engine, so an
// alternation such as "week" inside "weekday" behaves the same in both.
var (
	reEvery    = regexp.MustCompile(`(?i)\bevery\s+(day|week|month|year|weekday|(\d+)\s*(day|week|month)s?|[a-z]{3,9}day)\b`)
	rePriority = regexp.MustCompile(`(?i)\s(?:p|!!)([1-4])\b`)
	reProject  = regexp.MustCompile(`(?i)\s#([\w\-åäöõüšž]+)`)
	reLabel    = regexp.MustCompile(`(?i)\s@([\w\-åäöõüšž]+)`)
	reClock    = regexp.MustCompile(`\s(?:at\s+)?([01]?\d|2[0-3]):([0-5]\d)\b`)
	reAtHour   = regexp.MustCompile(`(?i)\sat\s+([01]?\d|2[0-3])\s*(am|pm)?\b`)
	reToday    = regexp.MustCompile(`(?i)\b(today|tod|tonight)\b`)
	reTomorrow = regexp.MustCompile(`(?i)\b(tomorrow|tom|tmr)\b`)
	reInDays   = regexp.MustCompile(`(?i)\bin\s+(\d+)\s*(day|days|week|weeks)\b`)
	reNextWeek = regexp.MustCompile(`(?i)\bnext\s+week\b`)
	reNextDay  = regexp.MustCompile(`(?i)\bnext\s+([a-z]{3,9})\b`)
	reWeekday  = regexp.MustCompile(`(?i)\b(mon|tue|wed|thu|fri|sat|sun)(?:day|sday|nesday|rsday|urday)?\b`)
	reDotted   = regexp.MustCompile(`\b(\d{1,2})\.(\d{1,2})(?:\.(\d{2,4}))?\b`)
	reDayMonth = regexp.MustCompile(`(?i)\b(\d{1,2})\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\b`)
	reSpaces   = regexp.MustCompile(`\s+`)
)

var (
	weekdayNames  = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	weekdayShort  = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
	weekdayRRule  = []string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}
	monthNames    = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
	intervalFreq  = map[string]string{"day": "DAILY", "week": "WEEKLY", "month": "MONTHLY"}
	fixedFreqName = map[string]string{
		"day":     "FREQ=DAILY",
		"week":    "FREQ=WEEKLY",
		"month":   "FREQ=MONTHLY",
		"year":    "FREQ=YEARLY",
		"weekday": "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR",
	}
)

const isoDay = "2006-01-02"

// Parse reads one line and returns the fields it found. today fixes the
// meaning of a relative word such as "tomorrow", so a test stays stable.
func Parse(text string, today time.Time) Result {
	out := Result{}
	rest := " " + text + " "

	// eat removes the first match from the line and gives the groups to fn. A
	// false from fn leaves the line alone, so the next pattern gets a try.
	eat := func(re *regexp.Regexp, fn func(m []string) bool) bool {
		loc := re.FindStringSubmatchIndex(rest)
		if loc == nil {
			return false
		}
		if !fn(groups(rest, loc)) {
			return false
		}
		out.Parsed = append(out.Parsed, strings.TrimSpace(rest[loc[0]:loc[1]]))
		rest = rest[:loc[0]] + " " + rest[loc[1]:]
		return true
	}

	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	plus := func(n int) string { return day.AddDate(0, 0, n).Format(isoDay) }

	// Recurrence comes first: "every tuesday" also sets the first due date, and
	// the weekday must not go to the date rules as a one-time date.
	eat(reEvery, func(m []string) bool {
		w := strings.ToLower(m[1])
		if r, ok := fixedFreqName[w]; ok {
			out.RRule = r
			return true
		}
		if m[2] != "" {
			out.RRule = "FREQ=" + intervalFreq[strings.ToLower(m[3])] + ";INTERVAL=" + m[2]
			return true
		}
		i := weekdayIndex(w)
		if i < 0 {
			return false
		}
		out.RRule = "FREQ=WEEKLY;BYDAY=" + weekdayRRule[i]
		if out.Due == "" {
			out.Due = nextWeekday(day, i, false)
		}
		return true
	})

	eat(rePriority, func(m []string) bool {
		out.Priority = atoi(m[1])
		return true
	})

	eat(reProject, func(m []string) bool {
		out.Project = m[1]
		return true
	})

	// A line carries several labels. The guard stops a pathological line.
	for guard := 0; guard < 10; guard++ {
		if !eat(reLabel, func(m []string) bool {
			out.Labels = append(out.Labels, m[1])
			return true
		}) {
			break
		}
	}

	_ = eat(reClock, func(m []string) bool {
		out.Time = pad2(atoi(m[1])) + ":" + m[2]
		return true
	}) || eat(reAtHour, func(m []string) bool {
		h := atoi(m[1])
		if strings.EqualFold(m[2], "pm") && h < 12 {
			h += 12
		}
		out.Time = pad2(h) + ":00"
		return true
	})

	if out.Due == "" {
		_ = eat(reToday, func([]string) bool {
			out.Due = plus(0)
			return true
		}) || eat(reTomorrow, func([]string) bool {
			out.Due = plus(1)
			return true
		}) || eat(reInDays, func(m []string) bool {
			step := 1
			if strings.HasPrefix(strings.ToLower(m[2]), "week") {
				step = 7
			}
			out.Due = plus(atoi(m[1]) * step)
			return true
		}) || eat(reNextWeek, func([]string) bool {
			out.Due = plus(7)
			return true
		}) || eat(reNextDay, func(m []string) bool {
			i := weekdayIndex(m[1])
			if i < 0 {
				return false
			}
			out.Due = nextWeekday(day, i, true)
			return true
		}) || eat(reWeekday, func(m []string) bool {
			i := weekdayIndex(m[1])
			if i < 0 {
				return false
			}
			out.Due = nextWeekday(day, i, false)
			return true
		}) || eat(reDotted, func(m []string) bool { // 24.12 or 03.02.2027
			year := day.Year()
			if m[3] != "" {
				year = atoi(m[3])
				if len(m[3]) == 2 {
					year += 2000
				}
			}
			d := time.Date(year, time.Month(atoi(m[2])), atoi(m[1]), 0, 0, 0, 0, time.UTC)
			// A day and a month with no year mean the next time that date comes.
			if m[3] == "" && d.Before(day) {
				d = d.AddDate(1, 0, 0)
			}
			out.Due = d.Format(isoDay)
			return true
		}) || eat(reDayMonth, func(m []string) bool { // 5 sep
			month := monthIndex(m[2])
			d := time.Date(day.Year(), time.Month(month+1), atoi(m[1]), 0, 0, 0, 0, time.UTC)
			if d.Before(day) {
				d = d.AddDate(1, 0, 0)
			}
			out.Due = d.Format(isoDay)
			return true
		})
	}

	out.Title = strings.TrimSpace(reSpaces.ReplaceAllString(rest, " "))
	return out
}

// groups turns the index pairs of a match into strings. A group that did not
// take part becomes an empty string.
func groups(s string, loc []int) []string {
	out := make([]string, len(loc)/2)
	for i := range out {
		if loc[2*i] >= 0 {
			out[i] = s[loc[2*i]:loc[2*i+1]]
		}
	}
	return out
}

func weekdayIndex(word string) int {
	w := strings.ToLower(word)
	for i, name := range weekdayNames {
		if w == name {
			return i
		}
	}
	if len(w) >= 3 {
		for i, name := range weekdayShort {
			if w[:3] == name {
				return i
			}
		}
	}
	return -1
}

func monthIndex(word string) int {
	w := strings.ToLower(word)
	for i, name := range monthNames {
		if w == name {
			return i
		}
	}
	return -1
}

// nextWeekday returns the next date with that weekday. A plain weekday name on
// that same weekday means today; "next" always steps a full week.
func nextWeekday(from time.Time, index int, forceNext bool) string {
	delta := (index - int(from.Weekday()) + 7) % 7
	if delta == 0 && forceNext {
		delta = 7
	}
	return from.AddDate(0, 0, delta).Format(isoDay)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
