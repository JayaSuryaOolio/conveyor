package core

import (
	"path/filepath"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestApplyDecisionLines(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r, _ := store.AddRepo(db, store.Repo{Name: "api", Path: "/src/api", DefaultBranch: "main"})
	tk, err := store.CreateTask(db, store.Task{Title: "T", Status: store.StatusTodo}, []store.TaskRepo{{RepoID: r.ID, IsPrimary: true, Branch: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	ApplyDecisionLines(db, tk.ID, []string{"a — x", "a — x"}, []string{"f — y (source: CI)"}, []string{"D1 | wrong", "junk", "D99 | gone"}, nil)
	ds, _ := store.ListDecisions(db, tk.ID)
	if len(ds) != 2 || ds[1].Kind != store.KindFinding || ds[0].DisputedBy != store.AuthorAgent || ds[0].Dispute != "wrong" {
		t.Fatalf("%+v", ds)
	}
	ApplyDecisionLines(db, tk.ID, nil, nil, nil, []string{"d1"})
	if ds, _ = store.ListDecisions(db, tk.ID); ds[0].DisputedBy != "" {
		t.Fatalf("not unmarked: %+v", ds)
	}
}
