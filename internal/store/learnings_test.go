package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func learningTestDB(t *testing.T) (*sql.DB, func(body, status string) int64, func(id int64) string) {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	add := func(body, status string) int64 {
		res, err := db.Exec(`INSERT INTO learnings (body, status, created_at) VALUES (?,?,'t')`, body, status)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	status := func(id int64) string { l, _ := GetLearning(db, id); return l.Status }
	return db, add, status
}

func TestRejectSamePrefix(t *testing.T) {
	db, add, status := learningTestDB(t)
	long := "Fresh worktrees lack frontend/node_modules; run npm ci first"
	a := add(long, LearningProposed)
	b := add("fresh worktrees lack frontend/node_modules, run npm ci before tsc!", LearningProposed) // case/punct differ
	acc := add(long+" (accepted)", LearningAccepted)
	other := add("wails lives in ~/go/bin and may not be on PATH", LearningProposed)
	n, err := RejectSamePrefix(db, a)
	if err != nil || n != 2 {
		t.Fatalf("rejected %d, %v; want 2", n, err)
	}
	if status(a) != LearningRejected || status(b) != LearningRejected {
		t.Fatal("matching proposed learnings must be rejected")
	}
	if status(acc) != LearningAccepted || status(other) != LearningProposed {
		t.Fatal("accepted and unrelated learnings must be untouched")
	}
}

func TestRejectCategory(t *testing.T) {
	db, add, status := learningTestDB(t)
	none, rtk, sed, keep := add("None.", LearningProposed), add("rtk grep mangles output", LearningProposed),
		add("macOS needs sed -i '' not sed -i", LearningProposed), add("SQLite needs SetMaxOpenConns(1)", LearningProposed)
	if n, err := RejectCategory(db, "placeholder"); err != nil || n != 1 || status(none) != LearningRejected {
		t.Fatalf("placeholder: %d %v", n, err)
	}
	if n, err := RejectCategory(db, "personal-env"); err != nil || n != 2 || status(rtk) != LearningRejected || status(sed) != LearningRejected {
		t.Fatalf("personal-env: %d %v", n, err)
	}
	if status(keep) != LearningProposed {
		t.Fatal("real learning must survive")
	}
	if _, err := RejectCategory(db, "nope"); err == nil {
		t.Fatal("unknown category must error")
	}
}

func TestInsertLearningIfNewNormalizedDedupe(t *testing.T) {
	db, add, _ := learningTestDB(t)
	add("Needs frontend/dist for go build", LearningRejected)
	for _, body := range []string{"needs frontend/dist for go build.", "NEEDS  frontend/dist -- for go build", "a different fact"} {
		if err := InsertLearningIfNew(db, 0, 0, nil, body, "t"); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM learnings`).Scan(&n)
	if n != 2 {
		t.Fatalf("%d rows, want 2 (reworded duplicates skipped, new fact kept)", n)
	}
}
