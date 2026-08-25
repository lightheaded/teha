// SPDX-License-Identifier: AGPL-3.0-or-later

package main

// The command line client. It keeps no local copy of the data: every command
// is one or two HTTP calls to the server, so a capture from a hotkey costs one
// round trip and nothing has to be reconciled later.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lightheaded/teha/id"
	"github.com/lightheaded/teha/internal/store"
	"github.com/lightheaded/teha/quickadd"
)

const defaultServer = "http://127.0.0.1:8637"

// noPull asks the sync endpoint for no rows back. The client holds no local
// copy, so a write does not need the whole account in the answer.
const noPull = int64(1) << 62

const usage = `teha: a task manager on the command line.

Usage:
  teha add "<one line>"       capture a task
  teha today                  show the tasks due today or earlier
  teha ls "<filter>"          show the tasks that match a filter
  teha done <id or fragment>  complete a task
  teha projects               list the projects with their open counts

A quick add line takes: today, tomorrow, friday, next week, 24.12, 5 sep,
at 9:30, p1, #Project, @label and "every monday".

Options:
  --server <url>   the server address. Default http://127.0.0.1:8637,
                   environment TEHA_SERVER.
  --limit <n>      how many tasks to show. Default 50.
  -h, --help       show this text.

The token comes from TEHA_TOKEN, or from $XDG_CONFIG_HOME/teha/token with
mode 600 ($HOME/.config/teha/token).

Examples:
  teha add "Book the ferry tomorrow at 9:30 p1 #Trip @call"
  teha ls "overdue | today" && teha done ferry
`

// errQuiet reports a failure whose message is on the screen already.
var errQuiet = errors.New("failed")

// runClient serves the command line client.
func runClient(args []string) int {
	err := runCommand(args, os.Stdout)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errQuiet):
		return 1
	default:
		fmt.Fprintln(os.Stderr, "teha: "+err.Error())
		return 1
	}
}

func runCommand(args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, usage)
		return nil
	}
	name := args[0]
	pos, opt, err := parseOptions(args[1:])
	if err != nil {
		return err
	}
	if opt.help {
		fmt.Fprint(out, usage)
		return nil
	}

	token, err := loadToken()
	if err != nil {
		return err
	}
	c := &client{
		server: strings.TrimSuffix(opt.server, "/"),
		token:  token,
		http:   &http.Client{Timeout: 15 * time.Second},
		color:  useColor(os.Stdout),
	}

	switch name {
	case "add":
		return c.add(strings.Join(pos, " "), out)
	case "today":
		return c.list("today", opt.limit, out)
	case "ls":
		return c.list(strings.Join(pos, " "), opt.limit, out)
	case "done":
		return c.done(strings.Join(pos, " "), out)
	case "projects":
		return c.projectList(out)
	default:
		return fmt.Errorf("unknown command %q. Run teha add --help", name)
	}
}

// --- options ----------------------------------------------------------------

type options struct {
	server string
	limit  int
	help   bool
}

// parseOptions reads options from any position, so "teha add <line> --server u"
// works as well as "teha add --server u <line>". A person types the text first.
func parseOptions(args []string) ([]string, options, error) {
	opt := options{server: envOr("TEHA_SERVER", defaultServer), limit: 50}
	var pos []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, inline, hasInline := strings.Cut(arg, "=")
		value := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("the option %s needs a value", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "--server", "-server":
			v, err := value()
			if err != nil {
				return nil, opt, err
			}
			opt.server = v
		case "--limit", "-limit":
			v, err := value()
			if err != nil {
				return nil, opt, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, opt, fmt.Errorf("the option --limit needs a positive number")
			}
			opt.limit = n
		case "-h", "--help":
			opt.help = true
		default:
			if strings.HasPrefix(arg, "--") {
				return nil, opt, fmt.Errorf("unknown option %q. Run teha add --help", arg)
			}
			pos = append(pos, arg)
		}
	}
	return pos, opt, nil
}

// --- the token --------------------------------------------------------------

// tokenPath returns the file that holds the device token.
func tokenPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "teha", "token")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "teha", "token")
}

// loadToken finds the device token. The value never reaches the screen, a log
// or an error message: an error names the file instead.
func loadToken() (string, error) {
	if v := strings.TrimSpace(os.Getenv("TEHA_TOKEN")); v != "" {
		return v, nil
	}
	path := tokenPath()
	if path == "" {
		return "", nil
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil // the server can run without a token in development mode
	}
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a token file", path)
	}
	// A group or other bit lets a second account read the token, so refuse the
	// file. An owner execute bit gives nobody else access and stays allowed.
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return "", fmt.Errorf("the token file %s has mode %#o, so other users can read it. Run: chmod 600 %s",
			path, perm, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// --- the HTTP client --------------------------------------------------------

type client struct {
	server string
	token  string
	http   *http.Client
	color  bool
}

type syncResponse struct {
	Version  int64           `json:"version"`
	Applied  []store.Result  `json:"applied"`
	Projects []store.Project `json:"projects"`
	Labels   []store.Label   `json:"labels"`
	Tasks    []store.Task    `json:"tasks"`
}

func (c *client) do(method, path string, body any, into any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.server+path, reader)
	if err != nil {
		return fmt.Errorf("the server address %q is not valid", c.server)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach the server at %s. Start it, or set --server", c.server)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("the server refused the token. Set TEHA_TOKEN, or write the token to %s", tokenPath())
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("the server answered %s: %s", resp.Status, serverMessage(data))
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("the answer from the server is not valid JSON")
	}
	return nil
}

// serverMessage takes the error text out of an answer, or gives the raw body.
func serverMessage(data []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		return e.Error
	}
	return strings.TrimSpace(string(data))
}

// apply sends commands and reports the first one the server refused.
func (c *client) apply(cmds []store.Command) error {
	var resp syncResponse
	body := map[string]any{"since": noPull, "commands": cmds}
	if err := c.do(http.MethodPost, "/v1/sync", body, &resp); err != nil {
		return err
	}
	for _, r := range resp.Applied {
		if !r.OK {
			return fmt.Errorf("the server refused the change: %s", r.Error)
		}
	}
	return nil
}

func (c *client) projects() ([]store.Project, error) {
	var resp struct {
		Projects []store.Project `json:"projects"`
	}
	err := c.do(http.MethodGet, "/v1/projects", nil, &resp)
	return resp.Projects, err
}

func (c *client) tasks(query string, limit int) ([]store.Task, error) {
	var resp struct {
		Tasks []store.Task `json:"tasks"`
	}
	path := "/v1/tasks?limit=" + strconv.Itoa(limit)
	if query != "" {
		path += "&filter=" + urlQueryEscape(query)
	}
	err := c.do(http.MethodGet, path, nil, &resp)
	return resp.Tasks, err
}

// pullAll reads the whole account. A count and a title search must see every
// task, and the tasks endpoint stops at 500 rows.
func (c *client) pullAll() (syncResponse, error) {
	var resp syncResponse
	body := map[string]any{"since": 0, "commands": []store.Command{}}
	err := c.do(http.MethodPost, "/v1/sync", body, &resp)
	return resp, err
}

// --- add --------------------------------------------------------------------

func (c *client) add(line string, out io.Writer) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return errors.New(`add needs a line, for example: teha add "Buy milk tomorrow"`)
	}
	p := quickadd.Parse(line, time.Now())
	if p.Title == "" {
		return errors.New("the line has no title left after the date and the tags")
	}

	var cmds []store.Command
	projectID := store.InboxID
	projectName := ""
	unmatched := ""
	if p.Project != "" {
		list, err := c.projects()
		if err != nil {
			return err
		}
		match, several := matchProject(list, p.Project)
		switch {
		case len(several) > 0:
			return fmt.Errorf("the name #%s matches %s. Write the full name", p.Project, strings.Join(several, ", "))
		case match != nil:
			projectID, projectName = match.ID, match.Name
		default:
			// An unknown name goes to the inbox, and the answer says so. The web
			// app behaves the same way, and a typo must never make a junk
			// project. Capture still succeeds, which is the rule that matters.
			unmatched = p.Project
		}
	}

	args := store.TaskArgs{ID: id.New("t"), Title: &p.Title, ProjectID: &projectID}
	if p.Due != "" {
		args.DueDate = &p.Due
	}
	if p.Time != "" {
		args.DueTime = &p.Time
	}
	if p.Priority != 0 {
		args.Priority = &p.Priority
	}
	if p.RRule != "" {
		args.RRule = &p.RRule
	}
	args.Labels = p.Labels
	cmds = append(cmds, newCommand("task_add", args))

	if err := c.apply(cmds); err != nil {
		return err
	}
	fmt.Fprintln(out, "added: "+summary(p, projectName))
	if unmatched != "" {
		fmt.Fprintf(out, "no project matches #%s, so the task is in the inbox\n", unmatched)
	}
	return nil
}

// summary is the one line a person reads after a capture.
func summary(p quickadd.Result, projectName string) string {
	var parts []string
	if p.Due != "" {
		parts = append(parts, "due "+humanDate(p.Due, p.Time))
	} else if p.Time != "" {
		parts = append(parts, "at "+p.Time)
	}
	if p.RRule != "" {
		parts = append(parts, "repeats "+p.RRule)
	}
	if p.Priority != 0 && p.Priority != 4 {
		parts = append(parts, "p"+strconv.Itoa(p.Priority))
	}
	if projectName != "" {
		parts = append(parts, "#"+projectName)
	}
	for _, l := range p.Labels {
		parts = append(parts, "@"+l)
	}
	if len(parts) == 0 {
		return p.Title
	}
	return p.Title + " — " + strings.Join(parts, ", ")
}

// matchProject resolves a name the way the web app does: an exact match first,
// then a single prefix match. An unclear prefix returns the candidates.
func matchProject(list []store.Project, name string) (*store.Project, []string) {
	low := strings.ToLower(name)
	for i := range list {
		if strings.ToLower(list[i].Name) == low {
			return &list[i], nil
		}
	}
	var hits []int
	for i := range list {
		if strings.HasPrefix(strings.ToLower(list[i].Name), low) {
			hits = append(hits, i)
		}
	}
	if len(hits) == 1 {
		return &list[hits[0]], nil
	}
	var names []string
	for _, i := range hits {
		names = append(names, list[i].Name)
	}
	return nil, names
}

func newCommand(kind string, args any) store.Command {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err) // the argument types are fixed, so this cannot happen
	}
	return store.Command{UUID: id.New("c"), Type: kind, Args: raw}
}

// --- list -------------------------------------------------------------------

func (c *client) list(query string, limit int, out io.Writer) error {
	tasks, err := c.tasks(query, limit)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Fprintln(out, "no tasks match")
		return nil
	}
	list, err := c.projects()
	if err != nil {
		return err
	}
	names := map[string]string{}
	for _, p := range list {
		if !p.IsInbox {
			names[p.ID] = p.Name
		}
	}
	today := time.Now().Format("2006-01-02")

	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	for _, t := range tasks {
		cells := []string{
			t.ID,
			c.priorityMark(t.Priority),
			c.dueCell(t, today),
			t.Title,
			project(names[t.ProjectID]),
			labels(t.Labels),
		}
		fmt.Fprintln(w, strings.Join(trimTail(cells), "\t"))
	}
	return w.Flush()
}

func project(name string) string {
	if name == "" {
		return ""
	}
	return "#" + name
}

func labels(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return "@" + strings.Join(list, " @")
}

// trimTail drops the empty cells at the end of a row, so a plain task does not
// carry trailing spaces into a pipe.
func trimTail(cells []string) []string {
	for len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return cells
}

func (c *client) priorityMark(p int) string {
	switch p {
	case 1:
		return c.paint("!!!", "31")
	case 2:
		return c.paint("!!", "33")
	case 3:
		return c.paint("!", "34")
	default:
		return ""
	}
}

func (c *client) dueCell(t store.Task, today string) string {
	if t.DueDate == nil || *t.DueDate == "" {
		if t.DueTime != nil {
			return *t.DueTime
		}
		return ""
	}
	text := humanDate(*t.DueDate, str(t.DueTime))
	if t.RRule != nil && *t.RRule != "" {
		text += " ~"
	}
	if *t.DueDate < today {
		return c.paint(text, "31")
	}
	return text
}

// humanDate writes a date the way a person reads it in a list.
func humanDate(day, clock string) string {
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	now := time.Now()
	text := d.Format("Mon 2 Jan")
	switch {
	case day == now.Format("2006-01-02"):
		text = "today"
	case day == now.AddDate(0, 0, 1).Format("2006-01-02"):
		text = "tomorrow"
	case d.Year() != now.Year():
		text = d.Format("2 Jan 2006")
	}
	if clock != "" {
		text += " " + clock
	}
	return text
}

// paint adds color, but only when a person is watching. A pipe or a file gets
// plain text.
func (c *client) paint(text, code string) string {
	if !c.color || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func useColor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// --- done -------------------------------------------------------------------

func (c *client) done(needle string, out io.Writer) error {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return errors.New("done needs an id or a part of a title, for example: teha done ferry")
	}
	all, err := c.pullAll()
	if err != nil {
		return err
	}
	open := openTasks(all.Tasks)
	matches := findTasks(open, needle)
	switch len(matches) {
	case 0:
		return fmt.Errorf("no open task matches %q", needle)
	case 1:
	default:
		fmt.Fprintf(out, "%q matches %d open tasks. Give the id or more of the title:\n", needle, len(matches))
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, t := range matches {
			fmt.Fprintln(w, strings.Join(trimTail([]string{t.ID, t.Title}), "\t"))
		}
		_ = w.Flush()
		return errQuiet
	}

	t := matches[0]
	if err := c.apply([]store.Command{newCommand("task_complete", store.IDArgs{ID: t.ID})}); err != nil {
		return err
	}
	if t.RRule != nil && *t.RRule != "" {
		fmt.Fprintln(out, "done: "+t.Title+" — it repeats, so the next date is set")
		return nil
	}
	fmt.Fprintln(out, "done: "+t.Title)
	return nil
}

func openTasks(tasks []store.Task) []store.Task {
	out := make([]store.Task, 0, len(tasks))
	for _, t := range tasks {
		if t.State == "open" && t.DeletedAt == nil {
			out = append(out, t)
		}
	}
	return out
}

// findTasks matches an id first, then a part of a title. An exact id always
// wins, so a copied id never completes the wrong task.
func findTasks(tasks []store.Task, needle string) []store.Task {
	for _, t := range tasks {
		if t.ID == needle {
			return []store.Task{t}
		}
	}
	low := strings.ToLower(needle)
	var out []store.Task
	for _, t := range tasks {
		if strings.Contains(strings.ToLower(t.Title), low) {
			out = append(out, t)
		}
	}
	return out
}

// --- projects ---------------------------------------------------------------

func (c *client) projectList(out io.Writer) error {
	all, err := c.pullAll()
	if err != nil {
		return err
	}
	count := map[string]int{}
	for _, t := range openTasks(all.Tasks) {
		count[t.ProjectID]++
	}
	list, err := c.projects()
	if err != nil {
		return err
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].IsInbox != list[j].IsInbox {
			return list[i].IsInbox
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tOPEN")
	for _, p := range list {
		fmt.Fprintf(w, "%s\t%d\n", p.Name, count[p.ID])
	}
	return w.Flush()
}

// --- small helpers ----------------------------------------------------------

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// urlQueryEscape encodes a filter for the query string. The filter grammar uses
// characters that a raw URL cannot carry.
func urlQueryEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.', ch == '~':
			b.WriteByte(ch)
		case ch == ' ':
			b.WriteByte('+')
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}
