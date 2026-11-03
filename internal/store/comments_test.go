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
	TakeQueuedComment(db, task.ID, c.ID)
	if err := ReleaseIfNothingQueued(db, task.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetTask(db, task.ID); got.Status != StatusNeedsYou {
		t.Fatalf("after take-back = %s", got.Status)
	}
}
