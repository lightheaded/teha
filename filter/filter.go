// SPDX-License-Identifier: Apache-2.0

// Package filter compiles a query in the teha filter language into a SQL
// WHERE clause.
//
// The grammar follows Todoist, so an imported filter keeps working:
//
//	today | tomorrow | overdue | od | no date | no time | recurring
//	subtask | !subtask | done | p1..p4 | no priority
//	#Project | ##Project | %label | @label | search: text
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

// Compile turns a query into a SQL fragment plus its arguments, with the names
// of the server database.
// today fixes the meaning of relative words, so a test is stable.
func Compile(query string, today time.Time) (string, []any, error) {
	return CompileFor(query, today, ServerSchema)
}

// CompileFor turns a query into a SQL fragment plus its arguments, with the
// names that s gives.
//
// One parser, two dialects. A client whose local store names the same rows
// differently passes its own Schema, so one filter string means one thing in
// every client and on the server. See Schema.
func CompileFor(query string, today time.Time, s Schema) (string, []any, error) {
	p := &parser{lex: lex(query), today: today, s: s}
	p.next()
	if p.tok.kind == tokEOF {
		// An empty query means every OPEN task, not every task. It used to
		// return here before the narrowing below, so `list_tasks` with no
		// filter and `teha ls` with no argument both answered with completed
		// rows as well.
		return s.State + " = 'open'", nil, nil
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
		sql = "(" + sql + ") AND " + s.State + " = 'open'"
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
	// s names the tables and the columns the output refers to.
	s Schema
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
	s := p.s

	switch low {
	case "today", "tod":
		return fmt.Sprintf("(%[1]s IS NOT NULL AND %[1]s <= ?)", s.DueDate), []any{day(0)}, nil
	case "tomorrow", "tom":
		return s.DueDate + " = ?", []any{day(1)}, nil
	case "yesterday":
		return s.DueDate + " = ?", []any{day(-1)}, nil
	case "overdue", "od", "over due":
		return fmt.Sprintf("(%[1]s IS NOT NULL AND %[1]s < ?)", s.DueDate), []any{day(0)}, nil
	case "no date", "no due date", "nodate":
		return s.DueDate + " IS NULL", nil, nil
	case "no time":
		return s.DueTime + " IS NULL", nil, nil
	case "has time":
		return s.DueTime + " IS NOT NULL", nil, nil
	case "recurring":
		return fmt.Sprintf("(%[1]s IS NOT NULL AND %[1]s <> '')", s.RRule), nil, nil
	case "subtask":
		return s.ParentID + " IS NOT NULL", nil, nil
	case "no parent", "top level":
		return s.ParentID + " IS NULL", nil, nil
	case "no priority":
		return s.Priority + " = 4", nil, nil
	case "no deadline":
		return s.Deadline + " IS NULL", nil, nil
	case "deadline":
		return s.Deadline + " IS NOT NULL", nil, nil
	case "no label", "no labels":
		return p.noLabels(), nil, nil
	case "started":
		return fmt.Sprintf("(%[1]s IS NULL OR %[1]s <= ?)", s.StartDate), []any{day(0)}, nil
	case "not started", "deferred":
		return fmt.Sprintf("(%[1]s IS NOT NULL AND %[1]s > ?)", s.StartDate), []any{day(0)}, nil
	case "done", "completed":
		p.saidState = true
		return s.State + " = 'done'", nil, nil
	case "wont do", "wont-do", "won't do", "skipped":
		p.saidState = true
		return s.State + " = 'wont_do'", nil, nil
	case "open", "active":
		p.saidState = true
		return s.State + " = 'open'", nil, nil
	case "any state", "all states":
		p.saidState = true
		return "1=1", nil, nil
	case "week", "next 7 days", "7 days":
		return fmt.Sprintf("(%[1]s IS NOT NULL AND %[1]s <= ?)", s.DueDate), []any{day(7)}, nil
	}

	// p1..p4
	if len(low) == 2 && low[0] == 'p' && low[1] >= '1' && low[1] <= '4' {
		n, _ := strconv.Atoi(low[1:])
		return s.Priority + " = ?", []any{n}, nil
	}

	// prefix forms
	switch {
	case strings.HasPrefix(word, "##"):
		name := strings.TrimSpace(word[2:])
		return fmt.Sprintf(`%s IN (WITH RECURSIVE tree(id) AS (
			SELECT %s FROM %s WHERE lower(%s) = lower(?) AND %s IS NULL
			UNION ALL SELECT p.%s FROM %s p JOIN tree t ON p.%s = t.id)
			SELECT id FROM tree)`,
			s.ProjectID, s.ID, s.Project, s.Name, s.DeletedAt,
			s.ID, s.Project, s.ParentID), []any{name}, nil
	case strings.HasPrefix(word, "#"):
		name := strings.TrimSpace(word[1:])
		if strings.EqualFold(name, "inbox") {
			return fmt.Sprintf("%s = '%s'", s.ProjectID, s.InboxID), nil, nil
		}
		if strings.HasSuffix(name, "*") {
			return fmt.Sprintf(`%s IN (SELECT %s FROM %s WHERE lower(%s) LIKE lower(?) AND %s IS NULL)`,
					s.ProjectID, s.ID, s.Project, s.Name, s.DeletedAt),
				[]any{strings.TrimSuffix(name, "*") + "%"}, nil
		}
		return fmt.Sprintf(`%s IN (SELECT %s FROM %s WHERE lower(%s) = lower(?) AND %s IS NULL)`,
			s.ProjectID, s.ID, s.Project, s.Name, s.DeletedAt), []any{name}, nil
	case strings.HasPrefix(word, "%"), strings.HasPrefix(word, "@"):
		// Todoist moved filters to %label and retires @ through 2026. Both work
		// here, so an imported filter and a habit both keep working.
		name := strings.TrimSpace(word[1:])
		if strings.HasSuffix(name, "*") {
			return p.labelTerm(strings.TrimSuffix(name, "*"), true)
		}
		return p.labelTerm(name, false)
	}

	// key: value forms
	if k, v, ok := splitKey(low, word); ok {
		switch k {
		case "search":
			// A LIKE over the title and the description, in both dialects. The
			// server has an fts5 table, and the Room database on the phone has
			// none, so a compiled filter never names one. The MCP search tool
			// reads fts5 on its own path.
			return p.search(v)
		case "date", "due":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return s.DueDate + " = ?", []any{d}, nil
		case "before", "date before", "due before":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("(%[1]s IS NOT NULL AND %[1]s < ?)", s.DueDate), []any{d}, nil
		case "after", "date after", "due after":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("(%[1]s IS NOT NULL AND %[1]s > ?)", s.DueDate), []any{d}, nil
		case "deadline":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return s.Deadline + " = ?", []any{d}, nil
		case "deadline before":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("(%[1]s IS NOT NULL AND %[1]s < ?)", s.Deadline), []any{d}, nil
		case "created before":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return p.created("<", d)
		case "created after":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return p.created(">", d)
		case "created":
			d, err := p.date(v)
			if err != nil {
				return "", nil, err
			}
			return p.created("=", d)
		}
	}

	// A bare word searches the title, which is what a person expects.
	return p.search(low)
}

// search matches text in the title or the description.
func (p *parser) search(text string) (string, []any, error) {
	like := "%" + strings.ToLower(text) + "%"
	return fmt.Sprintf("(lower(%s) LIKE ? OR lower(%s) LIKE ?)", p.s.Title, p.s.Description),
		[]any{like, like}, nil
}

// created compares the day a task was made. op is "<", ">" or "=".
func (p *parser) created(op, day string) (string, []any, error) {
	if p.s.CreatedAt == "" {
		return "", nil, fmt.Errorf("created: needs a creation date, and this client keeps none")
	}
	return fmt.Sprintf("date(%s) %s ?", p.s.CreatedAt, op), []any{day}, nil
}

// noLabels matches a task that carries no label at all.
func (p *parser) noLabels() string {
	s := p.s
	if s.Labels == "" {
		return fmt.Sprintf("%s NOT IN (SELECT %s FROM %s)", s.ID, s.TaskLabelTask, s.TaskLabel)
	}
	return fmt.Sprintf("(%[1]s IS NULL OR %[1]s = '')", s.Labels)
}

// labelTerm matches one label by name, or by a name prefix.
//
// Two stores answer this differently. The server joins the task_label and the
// label tables, so a name is an equality test. The phone keeps every label name
// of a task in one column, joined by a comma, so the test is a LIKE over a copy
// of that column with a comma added at each end. The pad is what makes the
// match exact: ",work," cannot match "homework".
//
// A name comes from a person, so it can hold a LIKE wildcard. The pattern
// therefore escapes "%", "_" and the escape character itself, and the SQL
// declares ESCAPE. Without that, the label "50%" would match every label.
//
// A name that holds a comma matches, because the pad and the join use the same
// separator. The reverse does not hold: two labels "a" and "b" also answer a
// query for a label named "a,b". A comma inside a label name is a defect of the
// joined column, which android/README.md records.
func (p *parser) labelTerm(name string, prefix bool) (string, []any, error) {
	s := p.s
	if s.Labels == "" {
		test := "="
		arg := name
		if prefix {
			test = "LIKE"
			arg = name + "%"
		}
		return fmt.Sprintf(`%s IN (SELECT tl.%s FROM %s tl JOIN %s l ON l.%s = tl.%s
			WHERE lower(l.%s) %s lower(?) AND l.%s IS NULL)`,
			s.ID, s.TaskLabelTask, s.TaskLabel, s.Label, s.ID, s.TaskLabelLabel,
			s.Name, test, s.DeletedAt), []any{arg}, nil
	}
	pattern := "%," + escapeLike(name)
	if prefix {
		pattern += "%"
	} else {
		pattern += ",%"
	}
	return fmt.Sprintf(`lower(',' || ifnull(%s,'') || ',') LIKE lower(?) ESCAPE '\'`, s.Labels),
		[]any{pattern}, nil
}

// escapeLike makes a literal out of text that a LIKE pattern holds.
//
// The order matters: the escape character goes first, or the escape this
// function adds for a "%" is escaped again.
func escapeLike(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, "%", `\%`)
	return strings.ReplaceAll(text, "_", `\_`)
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
