package store

import "testing"

func TestTakeQueuedComment(t *testing.T) {
	db := openTest(t)
	task := mkTask(t, db, StatusRunning)
	c, _ := AddComment(db, Comment{TaskID: task.ID, Author: AuthorHuman, Body: "hi"})
	if body, ok, err := TakeQueuedComment(db, task.ID, c.ID); err != nil || !ok || body != "hi" {
		t.Fatalf("got %q %v %v", body, ok, err)
	}
	if _, ok, _ := TakeQueuedComment(db, task.ID, c.ID); ok {
		t.Fatal("deleted comment taken twice")
	}
}

func TestReleaseIfNothingQueued(t *testing.T) {
	db := openTest(t)
	fresh := mkTask(t, db, StatusTodo) // never ran: stays todo
	if err := ReleaseIfNothingQueued(db, fresh.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetTask(db, fresh.ID); got.Status != StatusTodo {
		t.Fatalf("never-run task = %s", got.Status)
	}
	task := mkTask(t, db, StatusTodo)
	if _, err := CreateRun(db, Run{TaskID: task.ID, Kind: "auto", Mode: "fresh", Status: "finished"}); err != nil {
		t.Fatal(err)
	}
	c, _ := AddComment(db, Comment{TaskID: task.ID, Author: AuthorHuman, Body: "x"})
	if err := ReleaseIfNothingQueued(db, task.ID); err != nil { // comment still queued
		t.Fatal(err)
	}
	if got, _ := GetTask(db, task.ID); got.Status != StatusTodo {
		t.Fatalf("with queued comment = %s", got.Status)
	}
	AddComment(db, Comment{TaskID: task.ID, Author: AuthorSystem, Body: "The agy session was interrupted."})
	TakeQueuedComment(db, task.ID, c.ID)
	if err := ReleaseIfNothingQueued(db, task.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetTask(db, task.ID); got.Status != StatusNeedsYou {
		t.Fatalf("after take-back = %s", got.Status)
	}
}

func TestLatestRunsAndComments(t *testing.T) {
	db := openTest(t)
	a, b := mkTask(t, db, StatusTodo), mkTask(t, db, StatusTodo)
	CreateRun(db, Run{TaskID: a.ID, Kind: "auto", Mode: "fresh", Status: "finished"})
	last, _ := CreateRun(db, Run{TaskID: a.ID, Kind: "auto", Mode: "fresh", Status: "finished"})
	AddComment(db, Comment{TaskID: b.ID, Author: AuthorHuman, Body: "old"})
	AddComment(db, Comment{TaskID: b.ID, Author: AuthorAgent, Body: "new"})
	runs, err := LatestRuns(db)
	if err != nil || len(runs) != 1 || runs[a.ID].ID != last.ID {
		t.Fatalf("runs = %+v %v", runs, err)
	}
	msgs, err := LatestComments(db)
	if err != nil || len(msgs) != 1 || msgs[b.ID].Body != "new" {
		t.Fatalf("comments = %+v %v", msgs, err)
	}
}

func TestPostRunCommentUpdates(t *testing.T) {
	db := openTest(t)
	task := mkTask(t, db, StatusRunning)
	run, err := CreateRun(db, Run{TaskID: task.ID, Kind: "auto", Mode: "fresh", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{"question?", "reply 1", "reply 1", "final"} {
		if _, err := PostRunComment(db, Comment{TaskID: task.ID, Author: AuthorAgent, Body: b, RunID: &run.ID}); err != nil {
			t.Fatal(err)
		}
	}
	PostRunComment(db, Comment{TaskID: task.ID, Author: AuthorAgent, Body: "no run"})
	cs, _ := ListComments(db, task.ID)
	if len(cs) != 2 || cs[0].Body != "final" || len(cs[0].Edits) != 2 || cs[0].Edits[0].Body != "question?" || cs[0].Edits[1].Body != "reply 1" {
		t.Fatalf("got %+v", cs)
	}
}
