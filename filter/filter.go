// SPDX-License-Identifier: Apache-2.0

// Package filter compiles a query in the teha filter language into a SQL
// WHERE clause.
//
// The grammar follows Todoist, so an imported filter keeps working:
//
//	today | tomorrow | overdue | od | no date | no time | recurring
//	subtask | !subtask | done | p1..p4 | no priority
//	#Project | ##Project | /Section | no section | %label | @label | search: text
//	before: <date> | after: <date> | deadline | no deadline
//	& (and) | (or) ! (not) ( ) , (or, one saved filter shows several lists)
//
// The same grammar runs in the web app over the local database, so one filter
// string means the same thing in the app, in a saved view and in an MCP call.
package filter

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Compile turns a query into a SQL fragment plus its arguments.
// today fixes the meaning of relative words, so a test is stable.
func Compile(query string, today time.Time) (string, []any, error) {
	p := &parser{lex: lex(query), today: today}
	p.next()
	if p.tok.kind == tokEOF {
		// An empty query means every OPEN task, not every task. It used to
		// return here before the narrowing below, so `list_tasks` with no
		// filter and `teha ls` with no argument both answered with completed
		// rows as well.
		return "state = 'open'", nil, nil
	}
	sql, args, err := p.parseOr()
	if err != nil {
		return "", nil, err
	}
	if p.tok.kind != tokEOF {
		return "", nil, fmt.Errorf("unexpected %q at position %d", p.tok.text, p.tok.pos)
	}
	// A filter shows open tasks unless the query named a state itself.
	//
	// The test is what the PARSER saw, not what the text contains. A text
	// search for the word done, or a project named Done, used to switch the
	// narrowing off and silently mix completed rows into an ordinary view.
	if !p.saidState {
		sql = "(" + sql + ") AND state = 'open'"
	}
	return sql, args, nil
}

// --- lexer ------------------------------------------------------------------

type kind int

const (
	tokEOF kind = iota
	tokWord
	tokAnd
	tokOr
	tokNot
	tokLParen
	tokRParen
)

type token struct {
	kind kind
	text string
	pos  int
}

func lex(s string) []token {
	var out []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '&':
			out = append(out, token{tokAnd, "&", i})
			i++
		case c == '|', c == ',':
			out = append(out, token{tokOr, string(c), i})
			i++
		case c == '!':
			out = append(out, token{tokNot, "!", i})
			i++
		case c == '(':
			out = append(out, token{tokLParen, "(", i})
			i++
		case c == ')':
			out = append(out, token{tokRParen, ")", i})
			i++
		default:
			start := i
			for i < len(s) && !strings.ContainsRune("&|,!()", rune(s[i])) {
				i++
			}
			text := strings.TrimSpace(s[start:i])
			if text != "" {
				out = append(out, token{tokWord, text, start})
			}
		}
	}
	return append(out, token{tokEOF, "", len(s)})
}

type parser struct {
	lex   []token
	at    int
	tok   token
	today time.Time
	// saidState records that the query named a task state itself, so Compile
	// must not narrow to open tasks on top of it. A term sets this, not a
	// search of the query text: the text "search: done" and a project called
	// "Done" both contain the word and mean nothing about state.
	saidState bool
}

func (p *parser) next() {
	if p.at < len(p.lex) {
		p.tok = p.lex[p.at]
		p.at++
		return
	}
	p.tok = token{tokEOF, "", 0}
}

func (p *parser) parseOr() (string, []any, error) {
	left, args, err := p.parseAnd()
	if err != nil {
		return "", nil, err
	}
	for p.tok.kind == tokOr {
		p.next()
		right, rargs, err := p.parseAnd()
		if err != nil {
			return "", nil, err
		}
		left = "(" + left + " OR " + right + ")"
		args = append(args, rargs...)
	}
	return left, args, nil
}

func (p *parser) parseAnd() (string, []any, error) {
	left, args, err := p.parseUnary()
	if err != nil {
		return "", nil, err
	}
	for p.tok.kind == tokAnd {
		p.next()
		right, rargs, err := p.parseUnary()
		if err != nil {
			return "", nil, err
		}
		left = "(" + left + " AND " + right + ")"
		args = append(args, rargs...)
	}
	return left, args, nil
}

func (p *parser) parseUnary() (string, []any, error) {
	if p.tok.kind == tokNot {
		p.next()
		inner, args, err := p.parseUnary()
		if err != nil {
			return "", nil, err
		}
		return "NOT (" + inner + ")", args, nil
	}
	if p.tok.kind == tokLParen {
		p.next()
		inner, args, err := p.parseOr()
		if err != nil {
			return "", nil, err
		}
		if p.tok.kind != tokRParen {
			return "", nil, fmt.Errorf("missing closing parenthesis")
		}
		p.next()
		return "(" + inner + ")", args, nil
	}
	if p.tok.kind != tokWord {
		return "", nil, fmt.Errorf("expected a term, found %q", p.tok.text)
	}
	word := p.tok.text
	p.next()
	return p.term(word)
}

// term compiles one leaf of the query.
func (p *parser) term(word string) (string, []any, error) {
	low := strings.ToLower(strings.TrimSpace(word))
	day := func(d int) string { return p.today.AddDate(0, 0, d).Format("2006-01-02") }

	switch low {
	case "today", "tod":
		return "(due_date IS NOT NULL AND due_date <= ?)", []any{day(0)}, nil
	case "tomorrow", "tom":
		return "due_date = ?", []any{day(1)}, nil
	case "yesterday":
		return "due_date = ?", []any{day(-1)}, nil
	case "overdue", "od", "over due":
		return "(due_date IS NOT NULL AND due_date < ?)", []any{day(0)}, nil
	case "no date", "no due date", "nodate":
		return "due_date IS NULL", nil, nil
	case "no time":
		return "due_time IS NULL", nil, nil
	case "has time":
		return "due_time IS NOT NULL", nil, nil
	case "recurring":
		return "(rrule IS NOT NULL AND rrule <> '')", nil, nil
	case "subtask":
		return "parent_id IS NOT NULL", nil, nil
	case "no parent", "top level":
		return "parent_id IS NULL", nil, nil
	case "no priority":
		return "priority = 4", nil, nil
	case "no deadline":
		return "deadline IS NULL", nil, nil
	case "deadline":
		return "deadline IS NOT NULL", nil, nil
	case "no section", "no sections":
		return "section_id IS NULL", nil, nil
	case "no label", "no labels":
		return "id NOT IN (SELECT task_id FROM task_label)", nil, nil
	case "started":
		return "(start_date IS NULL OR start_date <= ?)", []any{day(0)}, nil
	case "not started", "deferred":
		return "(start_date IS NOT NULL AND start_date > ?)", []any{day(0)}, nil
	case "done", "completed":
		p.saidState = true
		return "state = 'done'", nil, nil
	case "wont do", "wont-do", "won't do", "skipped":
		p.saidState = true
		return "state = 'wont_do'", nil, nil
	case "open", "active":
		p.saidState = true
		return "state = 'open'", nil, nil
	case "any state", "all states":
		p.saidState = true
		return "1=1", nil, nil
	case "week", "next 7 days", "7 days":
		return "(due_date IS NOT NULL AND due_date <= ?)", []any{day(7)}, nil
	}

	// p1..p4
	if len(low) == 2 && low[0] == 'p' && low[1] >= '1' && low[1] <= '4' {
		n, _ := strconv.Atoi(low[1:])
		return "priority = ?", []any{n}, nil
	}

	// prefix forms
	switch {
	case strings.HasPrefix(word, "##"):
		name := strings.TrimSpace(word[2:])
		return `project_id IN (WITH RECURSIVE tree(id) AS (
			SELECT id FROM project WHERE lower(name) = lower(?) AND deleted_at IS NULL
			UNION ALL SELECT p.id FROM project p JOIN tree t ON p.parent_id = t.id)
			SELECT id FROM tree)`, []any{name}, nil
	case strings.HasPrefix(word, "#"):
		name := strings.TrimSpace(word[1:])
		if strings.EqualFold(name, "inbox") {
			return "project_id = 'inbox'", nil, nil
		}
		if strings.HasSuffix(name, "*") {
			return `project_id IN (SELECT id FROM project WHERE lower(name) LIKE lower(?) AND deleted_at IS NULL)`,
				[]any{strings.TrimSuffix(name, "*") + "%"}, nil
		}
		return `project_id IN (SELECT id FROM project WHERE lower(name) = lower(?) AND deleted_at IS NULL)`,
			[]any{name}, nil
	case strings.HasPrefix(word, "/"):
		// A section name follows the rules of a project name: an exact match,
		// case-insensitive, and a trailing * for a prefix match. "Errand" and
		// "Errands" are therefore two different sections, and a name that holds
		// a % or a _ is literal text in the exact form.
		name := strings.TrimSpace(word[1:])
		if strings.HasSuffix(name, "*") {
			return `section_id IN (SELECT id FROM section WHERE lower(name) LIKE lower(?) AND deleted_at IS NULL)`,
				[]any{strings.TrimSuffix(name, "*") + "%"}, nil
		}
		return `section_id IN (SELECT id FROM section WHERE lower(name) = lower(?) AND deleted_at IS NULL)`,
			[]any{name}, nil
	case strings.HasPrefix(word, "%"), strings.HasPrefix(word, "@"):
		// Todoist moved filters to %label and retires @ through 2026. Both work
		// here, so an imported filter and a habit both keep working.
		name := strings.TrimSpace(word[1:])
		if strings.HasSuffix(name, "*") {
			return `id IN (SELECT tl.task_id FROM task_label tl JOIN label l ON l.id = tl.label_id
				WHERE lower(l.name) LIKE lower(?) AND l.deleted_at IS NULL)`,
				[]any{strings.TrimSuffix(name, "*") + "%"}, nil
		}
		return `id IN (SELECT tl.task_id FROM task_label tl JOIN label l ON l.id = tl.label_id
			WHERE lower(l.name) = lower(?) AND l.deleted_at IS NULL)`, []any{name}, nil
	}

	// key: value forms
	if k, v, ok := splitKey(low, word); ok {
		switch k {
		case "search":
			like := "%" + strings.ToLower(v) + "%"
			return "(lower(title) LIKE ? OR lower(description) LIKE ?)", []any{like, like}, nil
		case "date", "due":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return "due_date = ?", []any{d}, nil
		case "before", "date before", "due before":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return "(due_date IS NOT NULL AND due_date < ?)", []any{d}, nil
		case "after", "date after", "due after":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return "(due_date IS NOT NULL AND due_date > ?)", []any{d}, nil
		case "deadline":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return "deadline = ?", []any{d}, nil
		case "deadline before":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return "(deadline IS NOT NULL AND deadline < ?)", []any{d}, nil
		case "created before":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return "date(created_at) < ?", []any{d}, nil
		case "created after":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return "date(created_at) > ?", []any{d}, nil
		case "created":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return "date(created_at) = ?", []any{d}, nil
		}
	}

	// A bare word searches the title, which is what a person expects.
	like := "%" + low + "%"
	return "(lower(title) LIKE ? OR lower(description) LIKE ?)", []any{like, like}, nil
}

func splitKey(low, raw string) (string, string, bool) {
	i := strings.Index(low, ":")
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(low[:i]), strings.TrimSpace(raw[i+1:]), true
}

// date reads an absolute or a relative date.
func (p *parser) date(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "today":
		return p.today.Format("2006-01-02"), nil
	case "tomorrow":
		return p.today.AddDate(0, 0, 1).Format("2006-01-02"), nil
	case "yesterday":
		return p.today.AddDate(0, 0, -1).Format("2006-01-02"), nil
	}
	// relative: "+5 days", "-3 days", "3 days"
	if strings.HasSuffix(v, "days") || strings.HasSuffix(v, "day") {
		num := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(v, "days"), "day"))
		if n, err := strconv.Atoi(strings.TrimPrefix(num, "+")); err == nil {
			return p.today.AddDate(0, 0, n).Format("2006-01-02"), nil
		}
	}
	for _, layout := range []string{"2006-01-02", "02.01.2006", "2.1.2006", "Jan 2 2006", "2 Jan 2006"} {
		if t, err := time.Parse(layout, strings.Title(v)); err == nil {
			return t.Format("2006-01-02"), nil
		}
	}
	// A weekday name means its next occurrence.
	for i, name := range []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"} {
		if v == name || v == name[:3] {
			delta := (i - int(p.today.Weekday()) + 7) % 7
			if delta == 0 {
				delta = 7
			}
			return p.today.AddDate(0, 0, delta).Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("cannot read the date %q", v)
}
