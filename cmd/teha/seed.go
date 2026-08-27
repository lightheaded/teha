// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/lightheaded/teha/internal/store"
)

// seedExample writes a small realistic tree, so the first run shows the app
// with content instead of an empty list.
//
// base fixes the day that every relative date counts from. A zero value means
// today, which is what a first run wants. The screenshot job passes a fixed day
// instead, because the app prints a weekday and a date on every row: with a
// moving base the same unchanged screen produces a different image every day,
// and the check that keeps the README current would fail on the calendar
// rather than on a change to the app.
func seedExample(st *store.Store, base time.Time) error {
	today := base
	if today.IsZero() {
		today = time.Now()
	}
	day := func(n int) string { return today.AddDate(0, 0, n).Format("2006-01-02") }

	type task struct {
		id       string
		title    string
		project  string
		section  string
		due      string
		priority int
		labels   []string
		rrule    string
		desc     string
	}

	projects := []struct{ id, name, color string }{
		{"p_home", "Home", "green"},
		{"p_shop", "Shopping", "orange"},
		{"p_trip", "Trip to Setomaa", "blue"},
	}
	// The sections of the trip project, so the board layout has columns.
	sections := []struct{ id, project, name string }{
		{"x_plan", "p_trip", "Plan"},
		{"x_book", "p_trip", "Booked"},
		{"x_pack", "p_trip", "Pack"},
	}
	// No task is added or removed here, and no field of a task changes except
	// the section and its order key. The other README screenshots therefore
	// stay byte for byte the same: a section shows in no list view, and the
	// sidebar counts do not move.
	tasks := []task{
		{"t_s1", "Book the guest house", "Trip to Setomaa", "x_book", day(2), 1, nil, "", "Two nights, check the sauna."},
		{"t_s2", "Print the route", "Trip to Setomaa", "x_plan", "", 4, nil, "", ""},
		{"t_s3", "Pack rain jackets", "Trip to Setomaa", "x_pack", day(6), 3, nil, "", ""},
		{"t_h1", "Change the water filter", "Home", "", day(-2), 2, nil, "FREQ=MONTHLY", ""},
		{"t_h2", "Take out the bins", "Home", "", day(0), 4, nil, "FREQ=WEEKLY;BYDAY=TU", ""},
		{"t_h3", "Call the plumber", "Home", "", day(0), 1, []string{"call"}, "", ""},
		{"t_g1", "Oat milk", "Shopping", "", "", 4, []string{"store"}, "", ""},
		{"t_g2", "Rye bread", "Shopping", "", "", 4, []string{"store"}, "", ""},
		{"t_g3", "Coffee beans", "Shopping", "", "", 4, []string{"store"}, "", ""},
		{"t_i1", "Read the plan and pick the first milestone", "", "", day(0), 2, nil, "", ""},
		{"t_i2", "Try the MCP server from a Claude session", "", "", day(1), 2, []string{"teha"}, "", ""},
	}

	cmds := []store.Command{}
	for _, p := range projects {
		cmds = append(cmds, cmd("project_add", store.ProjectArgs{
			ID: p.id, Name: &p.name, Color: &p.color,
		}))
	}
	for i, sec := range sections {
		cmds = append(cmds, cmd("section_add", store.SectionArgs{
			ID: sec.id, ProjectID: strptr(sec.project), Name: strptr(sec.name),
			OrderKey: strptr(fmt.Sprintf("m%06d", (i+1)*10)),
		}))
	}
	for i, t := range tasks {
		a := store.TaskArgs{ID: t.id, Title: strptr(t.title)}
		if t.section != "" {
			a.SectionID = strptr(t.section)
			// A board arranges work by hand, so a seeded task needs a key of
			// its own. Without one every card in a column shares the key "m"
			// and the order falls back to the title.
			a.OrderKey = strptr(fmt.Sprintf("m%06d", (i+1)*10))
		}
		if t.project != "" {
			a.Project = strptr(t.project)
		}
		if t.due != "" {
			a.DueDate = strptr(t.due)
		}
		if t.priority != 0 {
			a.Priority = &t.priority
		}
		if t.rrule != "" {
			a.RRule = strptr(t.rrule)
		}
		if t.desc != "" {
			a.Description = strptr(t.desc)
		}
		a.Labels = t.labels
		cmds = append(cmds, cmd("task_add", a))
	}

	v, results, err := st.Apply(cmds)
	if err != nil {
		return err
	}
	bad := 0
	for _, r := range results {
		if !r.OK {
			bad++
			fmt.Println("  seed error:", r.Error)
		}
	}
	fmt.Printf("  %d commands, %d failed, version %d\n", len(results), bad, v)
	return nil
}

func cmd(kind string, args any) store.Command {
	raw, _ := json.Marshal(args)
	return store.Command{UUID: kind + "-seed-" + fmt.Sprint(seedSeq()), Type: kind, Args: raw}
}

var seedCounter int

func seedSeq() int { seedCounter++; return seedCounter }

func strptr(s string) *string { return &s }
