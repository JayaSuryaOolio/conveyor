package store

import "testing"

func TestChildKey(t *testing.T) {
	for _, c := range []struct {
		parent string
		n      int
		want   string
	}{
		{"CV-1", 1, "CV-1A"},
		{"CV-1", 2, "CV-1B"},
		{"CV-1", 26, "CV-1Z"},
		{"CV-1", 27, "CV-1AA"},
		{"CV-1", 28, "CV-1AB"},
		{"CV-1", 52, "CV-1AZ"},
		{"CV-1", 53, "CV-1BA"},
		{"CV-1", 702, "CV-1ZZ"},
		{"CV-1", 703, "CV-1AAA"},
		{"CV-1A", 1, "CV-1A1"},
		{"CV-1A", 12, "CV-1A12"},
		{"CV-1A1", 1, "CV-1A1A"},
		{"CV-1AA", 3, "CV-1AA3"},
	} {
		if got := ChildKey(c.parent, c.n); got != c.want {
			t.Errorf("ChildKey(%q, %d) = %q, want %q", c.parent, c.n, got, c.want)
		}
	}
}

func TestChildKeysDontAffectTopLevelNumbering(t *testing.T) {
	db := openTest(t)
	r := mkRepo(t, db, "a")
	links := []TaskRepo{{RepoID: r.ID, IsPrimary: true, Branch: "t"}}
	p, err := CreateTask(db, Task{Title: "p"}, links)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CV-1A", "CV-1B"} {
		if c, err := CreateTask(db, Task{Title: "c", ParentID: &p.ID}, links); err != nil || c.Key != want {
			t.Fatalf("child = %q, %v; want %s", c.Key, err, want)
		}
	}
	a, _ := GetTaskByKey(db, "CV-1A")
	gc, err := CreateTask(db, Task{Title: "g", ParentID: &a.ID}, links)
	if err != nil || gc.Key != "CV-1A1" {
		t.Fatalf("grandchild = %q, %v", gc.Key, err)
	}
	next, err := CreateTask(db, Task{Title: "n"}, links)
	if err != nil || next.Key != "CV-2" {
		t.Fatalf("next top-level = %q, %v", next.Key, err)
	}
}

func TestLinks(t *testing.T) {
	db := openTest(t)
	a, b := mkTask(t, db, StatusBacklog), mkTask(t, db, StatusTodo)
	if err := AddLink(db, a.ID, a.ID, LinkBlocks); err == nil {
		t.Error("self link accepted")
	}
	if err := AddLink(db, a.ID, b.ID, "nope"); err == nil {
		t.Error("bad kind accepted")
	}
	if err := AddLink(db, a.ID, b.ID, LinkBlocks); err != nil {
		t.Fatal(err)
	}
	if got, _ := OpenBlockers(db, b.ID); len(got) != 1 || got[0] != a.Key {
		t.Fatalf("blockers = %v", got)
	}
	rel, err := ListRelations(db, b)
	if err != nil || len(rel.Links) != 1 || rel.Links[0].Label != "is blocked by" || rel.Links[0].Task.Key != a.Key {
		t.Fatalf("relations = %+v, %v", rel, err)
	}
	if _, err := UpdateTaskStatus(db, a.ID, StatusCancelled, ActorHuman); err != nil {
		t.Fatal(err)
	}
	if got, _ := OpenBlockers(db, b.ID); len(got) != 0 {
		t.Fatalf("cancelled task still blocks: %v", got)
	}
	if err := RemoveLink(db, a.ID, b.ID, LinkBlocks); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(db, a.ID, b.ID, LinkBlocks); err == nil {
		t.Error("removing a missing link succeeded")
	}
	if err := AddLink(db, a.ID, b.ID, LinkRelates); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(db, b.ID, a.ID, LinkRelates); err != nil {
		t.Fatalf("relates is symmetric: %v", err)
	}
}

func TestBlockLoops(t *testing.T) {
	db := openTest(t)
	a, b, c := mkTask(t, db, StatusTodo), mkTask(t, db, StatusTodo), mkTask(t, db, StatusTodo)
	for _, p := range [][2]int64{{a.ID, b.ID}, {b.ID, c.ID}} {
		if err := AddLink(db, p[0], p[1], LinkBlocks); err != nil {
			t.Fatal(err)
		}
	}
	if err := AddLink(db, c.ID, a.ID, LinkBlocks); err == nil {
		t.Error("loop a->b->c->a accepted")
	}
	if err := AddLink(db, b.ID, a.ID, LinkBlocks); err == nil {
		t.Error("loop a->b->a accepted")
	}
	if err := AddLink(db, a.ID, a.ID, LinkBlocks); err == nil {
		t.Error("a blocks a accepted")
	}
	if err := AddLink(db, c.ID, a.ID, LinkRelates); err != nil {
		t.Errorf("relates may point back: %v", err)
	}
	rel, err := ListRelations(db, b)
	if err != nil || len(rel.BlockedByAll) != 1 || rel.BlockedByAll[0] != a.Key || len(rel.BlocksAll) != 1 || rel.BlocksAll[0] != c.Key {
		t.Fatalf("reach = %+v, %v", rel, err)
	}
	if rel, _ = ListRelations(db, c); len(rel.BlockedByAll) != 2 || len(rel.BlocksAll) != 0 {
		t.Fatalf("c reach = %+v", rel)
	}
}

func TestSetParent(t *testing.T) {
	db := openTest(t)
	a, b, c := mkTask(t, db, StatusTodo), mkTask(t, db, StatusTodo), mkTask(t, db, StatusTodo)
	if err := SetParent(db, b.ID, &a.ID); err != nil {
		t.Fatal(err)
	}
	if err := SetParent(db, c.ID, &b.ID); err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]int64{{a.ID, a.ID}, {a.ID, b.ID}, {a.ID, c.ID}} {
		if err := SetParent(db, p[0], &p[1]); err == nil {
			t.Errorf("parent loop %d under %d accepted", p[0], p[1])
		}
	}
	rel, err := ListRelations(db, b)
	if err != nil || len(rel.Ancestors) != 1 || rel.Ancestors[0] != a.Key || len(rel.Descendants) != 1 || rel.Descendants[0] != c.Key {
		t.Fatalf("tree = %+v, %v", rel, err)
	}
	if err := SetParent(db, c.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetTaskByKey(db, c.Key); got.ParentID != nil {
		t.Fatalf("after clear: %+v", got)
	}
}
