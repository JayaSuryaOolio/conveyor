package jira

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestImportDedupes(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store.AddRepo(db, store.Repo{Name: "a", Path: "/x/a", DefaultBranch: "main"})
	bind := t.TempDir()
	argv := filepath.Join(bind, "argv")
	json := `[{"key":"OS-1","self":"https://api/1","fields":{"summary":"One","description":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Do it"}]}]}}},
{"key":"OS-2","self":"https://api/2","fields":{"summary":"Two","description":null}}]`
	os.WriteFile(filepath.Join(bind, "acli"), []byte("#!/bin/sh\necho \"$@\" > "+argv+"\ncat <<'EOF'\n"+json+"\nEOF\n"), 0o755)
	t.Setenv("PATH", bind+":"+os.Getenv("PATH"))
	svc := &core.Service{DB: db, Home: t.TempDir()}
	store.SetSetting(db, "jira_base_url", "https://x.atlassian.net/")

	keys, err := Import(context.Background(), svc, "project = OS", "a")
	if err != nil || len(keys) != 2 {
		t.Fatalf("%v %v", keys, err)
	}
	if b, _ := os.ReadFile(argv); string(b) != "jira workitem search --jql project = OS --fields key,summary,description --paginate --json\n" {
		t.Fatalf("argv: %q", b)
	}
	tk, _ := store.GetTaskByKey(db, keys[0])
	if tk.Source != "jira" || *tk.ExternalKey != "OS-1" || *tk.ExternalURL != "https://x.atlassian.net/browse/OS-1" || tk.Description != "Do it" || tk.Status != store.StatusBacklog {
		t.Fatalf("%+v", tk)
	}
	keys, err = Import(context.Background(), svc, "project = OS", "a")
	if err != nil || len(keys) != 0 {
		t.Fatalf("second import: %v %v", keys, err)
	}
	if ts, _ := store.ListTasks(db); len(ts) != 2 {
		t.Fatalf("%d tasks", len(ts))
	}
}
