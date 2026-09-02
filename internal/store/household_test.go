// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// The household. Two accounts in one file, and one promise: an account reads
// and writes what it owns and what somebody shared with it, and nothing else.
//
// Every test here starts from the owner and one invited person, because that
// is the household the app is for.

func twoAccounts(t *testing.T) (*Store, Account) {
	t.Helper()
	s := newStore(t)
	inv, err := s.CreateInvite(OwnerID, "partner", InviteTTL)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Code == "" {
		t.Fatal("an invitation must carry its code once")
	}
	partner, token, err := s.RedeemInvite(inv.Code, "Partner")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("a new account must get a device token")
	}
	return s, partner
}

// cmdOf builds one command with a fresh uuid, so a test can send several
// without naming each one.
var cmdSeq int

func cmdOf(kind string, args any) Command {
	cmdSeq++
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err) // the argument types are fixed, so this cannot happen
	}
	return Command{UUID: fmt.Sprintf("hh-%d", cmdSeq), Type: kind, Args: raw}
}

func applyAs(t *testing.T, s *Store, account string, cmds ...Command) []Result {
	t.Helper()
	_, res, err := s.ApplyAs(cmds, account)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return res
}

func mustApplyAs(t *testing.T, s *Store, account string, cmds ...Command) {
	t.Helper()
	for _, r := range applyAs(t, s, account, cmds...) {
		if !r.OK {
			t.Fatalf("command failed: %s", r.Error)
		}
	}
}

func TestAnInvitationMakesASecondAccountWithItsOwnInbox(t *testing.T) {
	s, partner := twoAccounts(t)

	if partner.ID == OwnerID {
		t.Fatal("the second account must not be the owner")
	}
	if partner.InboxID == InboxID {
		t.Fatalf("each account needs an inbox of its own, got %q", partner.InboxID)
	}
	owner, err := s.Owner()
	if err != nil {
		t.Fatal(err)
	}
	if owner.InboxID != InboxID {
		t.Fatalf("the owner keeps the fixed inbox id, got %q", owner.InboxID)
	}

	// A capture with no project lands in the inbox of the person who wrote it.
	mustApplyAs(t, s, partner.ID, cmdOf("task_add", TaskArgs{ID: "pt1", Title: ptr("Milk")}))
	got := oneTask(t, s, "pt1")
	if got.ProjectID != partner.InboxID {
		t.Fatalf("the task landed in %q, want the inbox of the person who wrote it", got.ProjectID)
	}
}

func TestAnInvitationWorksOnceAndExpires(t *testing.T) {
	s := newStore(t)
	inv, err := s.CreateInvite(OwnerID, "partner", InviteTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RedeemInvite(inv.Code, "Partner"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RedeemInvite(inv.Code, "Somebody else"); !errors.Is(err, ErrBadInvite) {
		t.Fatalf("a used invitation must not work twice, got %v", err)
	}
	if _, _, err := s.RedeemInvite("not-a-code", "Nobody"); !errors.Is(err, ErrBadInvite) {
		t.Fatalf("a made-up code must not work, got %v", err)
	}

	// An invitation that ran out is refused, and it says nothing more than
	// that a valid one would.
	old, err := s.CreateInvite(OwnerID, "late", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, _, err := s.RedeemInvite(old.Code, "Late"); !errors.Is(err, ErrBadInvite) {
		t.Fatalf("an expired invitation must not work, got %v", err)
	}
}

func TestOneAccountCannotSeeTheOther(t *testing.T) {
	s, partner := twoAccounts(t)

	mustApplyAs(t, s, OwnerID,
		cmdOf("project_add", ProjectArgs{ID: "p_work", Name: ptr("Work")}),
		cmdOf("task_add", TaskArgs{ID: "t_work", Title: ptr("Write the report"), ProjectID: ptr("p_work")}))
	mustApplyAs(t, s, partner.ID,
		cmdOf("project_add", ProjectArgs{ID: "p_study", Name: ptr("Study")}),
		cmdOf("task_add", TaskArgs{ID: "t_study", Title: ptr("Read chapter two"), ProjectID: ptr("p_study")}))

	ownerSees := pullIDs(t, s, OwnerID)
	partnerSees := pullIDs(t, s, partner.ID)

	if !slices.Contains(ownerSees, "t_work") || slices.Contains(ownerSees, "t_study") {
		t.Fatalf("the owner sees %v, want the work task and not the study task", ownerSees)
	}
	if !slices.Contains(partnerSees, "t_study") || slices.Contains(partnerSees, "t_work") {
		t.Fatalf("the partner sees %v, want the study task and not the work task", partnerSees)
	}
}

func TestOneAccountCannotWriteIntoTheOther(t *testing.T) {
	s, partner := twoAccounts(t)
	mustApplyAs(t, s, OwnerID,
		cmdOf("project_add", ProjectArgs{ID: "p_work", Name: ptr("Work")}),
		cmdOf("task_add", TaskArgs{ID: "t_work", Title: ptr("Write the report"), ProjectID: ptr("p_work")}))

	cases := []struct {
		name string
		cmd  Command
	}{
		{"add a task into a list they cannot see",
			cmdOf("task_add", TaskArgs{ID: "x1", Title: ptr("Sneak"), ProjectID: ptr("p_work")})},
		{"edit a task they cannot see",
			cmdOf("task_update", TaskArgs{ID: "t_work", Title: ptr("Changed")})},
		{"complete a task they cannot see",
			cmdOf("task_complete", IDArgs{ID: "t_work"})},
		{"delete a task they cannot see",
			cmdOf("task_delete", IDArgs{ID: "t_work"})},
		{"rename a list they cannot see",
			cmdOf("project_update", ProjectArgs{ID: "p_work", Name: ptr("Mine now")})},
		{"delete a list they cannot see",
			cmdOf("project_delete", IDArgs{ID: "p_work"})},
		{"add a section to a list they cannot see",
			cmdOf("section_add", SectionArgs{ID: "s1", ProjectID: ptr("p_work"), Name: ptr("Later")})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := applyAs(t, s, partner.ID, c.cmd)
			if res[0].OK {
				t.Fatal("the command was accepted, and it must be refused")
			}
			if !strings.Contains(res[0].Error, "cannot reach") && !strings.Contains(res[0].Error, "belongs to somebody") {
				t.Fatalf("the refusal must say nothing about the row, got %q", res[0].Error)
			}
		})
	}

	// Nothing changed.
	got := oneTask(t, s, "t_work")
	if got.Title != "Write the report" || got.State != "open" || got.DeletedAt != nil {
		t.Fatalf("a refused command still wrote something: %+v", got)
	}
}

func TestASharedProjectReachesBothPeople(t *testing.T) {
	s, partner := twoAccounts(t)
	mustApplyAs(t, s, OwnerID,
		cmdOf("project_add", ProjectArgs{ID: "p_shop", Name: ptr("Shopping")}),
		cmdOf("task_add", TaskArgs{ID: "t_milk", Title: ptr("Milk"), ProjectID: ptr("p_shop")}))

	if slices.Contains(pullIDs(t, s, partner.ID), "t_milk") {
		t.Fatal("the list is not shared yet")
	}
	if err := s.ShareProject("p_shop", partner.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(pullIDs(t, s, partner.ID), "t_milk") {
		t.Fatal("a shared list must reach the other person")
	}

	// A member works inside the list: they add, edit and complete.
	mustApplyAs(t, s, partner.ID,
		cmdOf("task_add", TaskArgs{ID: "t_bread", Title: ptr("Bread"), ProjectID: ptr("p_shop")}),
		cmdOf("task_complete", IDArgs{ID: "t_milk"}))
	if oneTask(t, s, "t_milk").State != "done" {
		t.Fatal("a member must be able to tick an item in a shared list")
	}
	if !slices.Contains(pullIDs(t, s, OwnerID), "t_bread") {
		t.Fatal("what a member adds must reach the owner")
	}

	// The list itself stays the owner's. A member does not rename it or take
	// it away from everybody.
	res := applyAs(t, s, partner.ID, cmdOf("project_update", ProjectArgs{ID: "p_shop", Name: ptr("Mine")}))
	if res[0].OK {
		t.Fatal("a member renamed a list that is not theirs")
	}
}

func TestUnsharingTellsTheClientToStartAgain(t *testing.T) {
	s, partner := twoAccounts(t)
	mustApplyAs(t, s, OwnerID,
		cmdOf("project_add", ProjectArgs{ID: "p_shop", Name: ptr("Shopping")}),
		cmdOf("task_add", TaskArgs{ID: "t_milk", Title: ptr("Milk"), ProjectID: ptr("p_shop")}))
	if err := s.ShareProject("p_shop", partner.ID); err != nil {
		t.Fatal(err)
	}

	// The partner is up to date.
	d, err := s.PullFor(0, partner.ID)
	if err != nil {
		t.Fatal(err)
	}
	at := d.Version

	if err := s.UnshareProject("p_shop", partner.ID); err != nil {
		t.Fatal(err)
	}
	after, err := s.PullFor(at, partner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Reset {
		t.Fatal("a pull across an unshare must ask the client to start again")
	}
	for _, task := range after.Tasks {
		if task.ID == "t_milk" {
			t.Fatal("the task is out of reach and must not be in the fresh pull")
		}
	}
	// The owner is untouched by any of it.
	if !slices.Contains(pullIDs(t, s, OwnerID), "t_milk") {
		t.Fatal("unsharing must not take the task from its owner")
	}
}

func TestAReminderBelongsToOnePerson(t *testing.T) {
	s, partner := twoAccounts(t)
	mustApplyAs(t, s, OwnerID,
		cmdOf("project_add", ProjectArgs{ID: "p_shop", Name: ptr("Shopping")}),
		cmdOf("task_add", TaskArgs{ID: "t_milk", Title: ptr("Milk"), ProjectID: ptr("p_shop"),
			DueDate: ptr("2026-09-01")}))
	if err := s.ShareProject("p_shop", partner.ID); err != nil {
		t.Fatal(err)
	}

	fire := "2026-09-01T06:00:00Z"
	mustApplyAs(t, s, OwnerID, cmdOf("reminder_add", ReminderArgs{
		ID: "r_owner", TaskID: ptr("t_milk"), Kind: ptr(KindAtDue), FireAt: &fire}))

	d, err := s.PullFor(0, partner.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range d.Reminders {
		if r.ID == "r_owner" {
			t.Fatal("a reminder is personal, even on a shared task")
		}
	}

	// And the other person cannot delete it.
	res := applyAs(t, s, partner.ID, cmdOf("reminder_delete", IDArgs{ID: "r_owner"}))
	if res[0].OK {
		t.Fatal("one person deleted another person's reminder")
	}
}

func TestSearchAndOneTaskStayInsideTheAccount(t *testing.T) {
	s, partner := twoAccounts(t)
	mustApplyAs(t, s, OwnerID,
		cmdOf("project_add", ProjectArgs{ID: "p_work", Name: ptr("Work")}),
		cmdOf("task_add", TaskArgs{ID: "t_secret", Title: ptr("Ferry to Saaremaa"),
			ProjectID: ptr("p_work")}))
	mustApplyAs(t, s, partner.ID,
		cmdOf("task_add", TaskArgs{ID: "t_mine", Title: ptr("Ferry timetable")}))

	// The full-text index holds both, and each account reads one.
	all, err := s.Search("ferry", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("the index holds %v, want both tasks", all)
	}
	got, err := s.SearchFor("ferry", 10, partner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"t_mine"}) {
		t.Fatalf("the partner searched and found %v", got)
	}

	// One task by id is the same question asked another way.
	if _, err := s.TaskFor("t_secret", partner.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("a task of another account came back by id: %v", err)
	}
	if _, err := s.TaskFor("t_mine", partner.ID); err != nil {
		t.Fatalf("a task of their own must come back: %v", err)
	}
}

func TestADeviceTokenNamesItsAccount(t *testing.T) {
	s := newStore(t)
	if err := s.SetAccountToken(OwnerID, "owner-token"); err != nil {
		t.Fatal(err)
	}
	inv, err := s.CreateInvite(OwnerID, "partner", InviteTTL)
	if err != nil {
		t.Fatal(err)
	}
	partner, token, err := s.RedeemInvite(inv.Code, "Partner")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.AccountForToken("owner-token")
	if err != nil || got.ID != OwnerID {
		t.Fatalf("the owner token names %v, %v", got.ID, err)
	}
	got, err = s.AccountForToken(token)
	if err != nil || got.ID != partner.ID {
		t.Fatalf("the partner token names %v, %v", got.ID, err)
	}
	if _, err := s.AccountForToken("wrong"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a wrong token must name nobody, got %v", err)
	}
	if _, err := s.AccountForToken(""); !errors.Is(err, ErrNotFound) {
		t.Fatal("an empty token must name nobody")
	}
}

func TestASessionSurvivesARestartAndThenExpires(t *testing.T) {
	s := newStore(t)
	value, err := s.NewSession(OwnerID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionAccount(value)
	if err != nil || got.ID != OwnerID {
		t.Fatalf("the session names %v, %v", got.ID, err)
	}

	// The value itself is not in the table. Only its hash is.
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM session WHERE id = ?`, value).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the cookie value must never be stored")
	}

	s.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := s.SessionAccount(value); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an expired session must not sign anybody in, got %v", err)
	}
}

// --- helpers ----------------------------------------------------------------

func pullIDs(t *testing.T, s *Store, accountID string) []string {
	t.Helper()
	d, err := s.PullFor(0, accountID)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, task := range d.Tasks {
		out = append(out, task.ID)
	}
	return out
}

func oneTask(t *testing.T, s *Store, id string) Task {
	t.Helper()
	d, err := s.Pull(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range d.Tasks {
		if task.ID == id {
			return task
		}
	}
	// A task in another account's list is not in the owner's pull, so read the
	// row itself. This helper is for asserting that nothing changed.
	var got Task
	var fromComplete int
	err = s.db.QueryRow(`SELECT `+taskCols+` FROM task WHERE id = ?`, id).Scan(
		&got.ID, &got.ProjectID, &got.SectionID, &got.ParentID, &got.OrderKey, &got.Title,
		&got.Description, &got.Priority, &got.DueDate, &got.DueTime, &got.DueTz, &got.RRule,
		&fromComplete, &got.StartDate, &got.Deadline, &got.DurationMin, &got.AssigneeID,
		&got.State, &got.CompletedAt, &got.DeletedAt, &got.SourceRef, &got.Version)
	if err != nil {
		t.Fatalf("no task %q: %v", id, err)
	}
	return got
}
