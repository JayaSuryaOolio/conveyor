package store

import (
	"slices"
	"testing"
)

func TestLabels(t *testing.T) {
	db := openTest(t)
	a, b := mkTask(t, db, StatusBacklog), mkTask(t, db, StatusBacklog)
	for _, n := range []string{"bug", " Bug ", "ui"} {
		if err := AddLabel(db, a.ID, n); err != nil {
			t.Fatal(err)
		}
	}
	if err := AddLabel(db, b.ID, "ui"); err != nil {
		t.Fatal(err)
	}
	if err := AddLabel(db, a.ID, " "); err == nil {
		t.Fatal("empty label accepted")
	}
	if got, _ := TaskLabels(db, a.ID); !slices.Equal(got, []string{"bug", "ui"}) {
		t.Fatalf("a labels = %v", got)
	}
	ts, _ := ListTasks(db)
	if !slices.Equal(ts[0].Labels, []string{"bug", "ui"}) || !slices.Equal(ts[1].Labels, []string{"ui"}) {
		t.Fatalf("list labels = %v %v", ts[0].Labels, ts[1].Labels)
	}
	RemoveLabel(db, a.ID, "UI")
	if got, _ := TaskLabels(db, a.ID); !slices.Equal(got, []string{"bug"}) {
		t.Fatalf("after remove = %v", got)
	}
	if all, _ := ListLabels(db); !slices.Equal(all, []string{"bug", "ui"}) {
		t.Fatalf("all = %v", all)
	}
}
