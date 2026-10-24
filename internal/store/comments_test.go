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
