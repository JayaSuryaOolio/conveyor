package store

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestMigrate_Idempotent(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate call should be a no-op, got error: %v", err)
	}
}

func TestCreateGetListTask(t *testing.T) {
	db := newTestDB(t)

	created, err := CreateTask(db, Task{Title: "Do the thing", Source: "manual"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated id")
	}
	if created.Status != StatusDraft {
		t.Errorf("expected default status draft, got %s", created.Status)
	}
	if created.Mode != "headless" {
		t.Errorf("expected default mode headless, got %s", created.Mode)
	}

	got, err := GetTask(db, created.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Title != "Do the thing" {
		t.Errorf("expected title round-trip, got %q", got.Title)
	}

	list, err := ListTasks(db)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("expected list to contain the created task, got %+v", list)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	db := newTestDB(t)
	if _, err := GetTask(db, "does-not-exist"); err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestUpdateTaskStatus_ValidTransition(t *testing.T) {
	db := newTestDB(t)
	task, err := CreateTask(db, Task{Title: "T", Source: "manual"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	updated, err := UpdateTaskStatus(db, task.ID, StatusGrooming, ActorHuman)
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if updated.Status != StatusGrooming {
		t.Errorf("expected status grooming, got %s", updated.Status)
	}

	reloaded, err := GetTask(db, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if reloaded.Status != StatusGrooming {
		t.Errorf("expected persisted status grooming, got %s", reloaded.Status)
	}
}

func TestUpdateTaskStatus_IllegalTransitionRejected(t *testing.T) {
	db := newTestDB(t)
	task, err := CreateTask(db, Task{Title: "T", Source: "manual"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if _, err := UpdateTaskStatus(db, task.ID, StatusDone, ActorSystem); err == nil {
		t.Fatal("expected draft -> done to be rejected")
	}

	reloaded, err := GetTask(db, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if reloaded.Status != StatusDraft {
		t.Errorf("expected status to remain draft after rejected transition, got %s", reloaded.Status)
	}
}

func TestUpdateTaskStatus_CancelledRejectsSystemActor(t *testing.T) {
	db := newTestDB(t)
	task, err := CreateTask(db, Task{Title: "T", Source: "manual"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := UpdateTaskStatus(db, task.ID, StatusCancelled, ActorSystem); err == nil {
		t.Fatal("expected system actor to be rejected from cancelling")
	}
}

func TestRecordEvent(t *testing.T) {
	db := newTestDB(t)
	task, err := CreateTask(db, Task{Title: "T", Source: "manual"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := RecordEvent(db, task.ID, "claimed", "worker-1"); err != nil {
		t.Fatalf("record event: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM kanban_task_events WHERE task_id = ?`, task.ID).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 event, got %d", count)
	}
}

func TestExternalKey_UniqueConstraint(t *testing.T) {
	db := newTestDB(t)
	if _, err := CreateTask(db, Task{Title: "A", Source: "jira", ExternalKey: "PROJ-1"}); err != nil {
		t.Fatalf("create first task: %v", err)
	}
	if _, err := CreateTask(db, Task{Title: "B", Source: "jira", ExternalKey: "PROJ-1"}); err == nil {
		t.Fatal("expected duplicate external_key to be rejected")
	}
	// Multiple tasks with no external_key (manual source) must still be allowed —
	// the unique index is partial (WHERE external_key IS NOT NULL).
	if _, err := CreateTask(db, Task{Title: "C", Source: "manual"}); err != nil {
		t.Errorf("create manual task with no external_key: %v", err)
	}
	if _, err := CreateTask(db, Task{Title: "D", Source: "manual"}); err != nil {
		t.Errorf("create second manual task with no external_key: %v", err)
	}
}
