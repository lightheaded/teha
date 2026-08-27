// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"path/filepath"
	"testing"
)

// TestDeletingALabelMovesEveryTaskThatCarriesIt is a regression test.
//
// The property test in property_test.go found this on seed 7: a device that
// already held a task kept showing a label that the account had deleted. Pull
// hides a deleted label from the label list of a task, so the label list of a
// task changes without the task row changing. A client pulls with
// `version > since`, so it never saw the task again and never dropped the
// label. The row was correct on the server and wrong on every client that had
// already synced, which is the hardest kind of divergence to notice.
func TestDeletingALabelMovesEveryTaskThatCarriesIt(t *testing.T) {
	s := openStoreAt(t, filepath.Join(t.TempDir(), "labels.db"))

	_, res, err := s.Apply([]Command{
		mkCmd("c1", "label_add", LabelArgs{ID: "l_shop", Name: ptr("shop")}),
		mkCmd("c2", "task_add", TaskArgs{ID: "t_1", Title: ptr("Buy oat milk"), Labels: []string{"shop"}}),
		mkCmd("c3", "task_add", TaskArgs{ID: "t_2", Title: ptr("Book the ferry")}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if !r.OK {
			t.Fatalf("%s failed: %s", r.UUID, r.Error)
		}
	}

	// A device syncs, so it holds the task with the label.
	d := newDevice("phone")
	d.pull(t, s)
	if got := d.tasks["t_1"].Labels; len(got) != 1 || got[0] != "shop" {
		t.Fatalf("the device holds labels %v, want [shop]", got)
	}
	taskVersion := d.tasks["t_1"].Version

	if _, res, err := s.Apply([]Command{mkCmd("c4", "label_delete", IDArgs{ID: "l_shop"})}); err != nil {
		t.Fatal(err)
	} else if !res[0].OK {
		t.Fatalf("the delete failed: %s", res[0].Error)
	}

	// The device pulls again, and it must learn that the label is gone.
	d.pull(t, s)
	if got := d.tasks["t_1"].Labels; len(got) != 0 {
		t.Errorf("after the label was deleted the device still shows %v on the task. "+
			"A label delete must move the version of every task that carries it", got)
	}
	if d.tasks["t_1"].Version == taskVersion {
		t.Errorf("the task stayed at version %d, so no pull can ever correct it", taskVersion)
	}

	// A task that never carried the label must not move, because a needless
	// version bump makes every client pull a row that did not change.
	if _, ok := d.tasks["t_2"]; !ok {
		t.Fatal("the second task vanished")
	}

	// The device and the server must agree, which is the property the walk in
	// property_test.go checks over a random stream.
	if got, want := d.state(), snapshot(t, s); got != want {
		t.Errorf("the device and the server disagree.\n%s", firstDifference(want, got))
	}
}
