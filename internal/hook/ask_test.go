package hook

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestIsAsk(t *testing.T) {
	claudePayload := []byte(`{"tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Which database?","options":[{"label":"sqlite","description":"local"}]}]}}`)
	if !IsAsk(claudePayload) {
		t.Fatal("expected IsAsk true for Claude AskUserQuestion")
	}

	agyPayload := []byte(`{"toolCall":{"id":"1","name":"ask_question","args":{"questions":[{"question":"Which database?","options":["sqlite","pg"]}]}}}`)
	if !IsAsk(agyPayload) {
		t.Fatal("expected IsAsk true for agy ask_question")
	}

	agyDefaultApi := []byte(`{"toolCall":{"id":"2","name":"default_api:ask_question","args":{"questions":[{"question":"Proceed?"}]}}}`)
	if !IsAsk(agyDefaultApi) {
		t.Fatal("expected IsAsk true for default_api:ask_question")
	}

	otherTool := []byte(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`)
	if IsAsk(otherTool) {
		t.Fatal("expected IsAsk false for Bash")
	}
}

func TestOnAsk(t *testing.T) {
	home := t.TempDir()
	db, err := store.Open(core.DBPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	r, err := store.AddRepo(db, store.Repo{Name: "repo", Path: "/x/repo", DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}

	tk, err := store.CreateTask(db, store.Task{Key: "CV-1", Title: "Test", Status: store.StatusRunning}, []store.TaskRepo{{RepoID: r.ID, IsPrimary: true, Branch: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	pid := "123"
	_, err = store.CreateRun(db, store.Run{TaskID: tk.ID, Kind: "auto", Mode: "fresh", Status: "running", BgID: &pid})
	if err != nil {
		t.Fatal(err)
	}

	worktree := filepath.Join(home, "workspaces", "CV-1", "repo")
	agyPayload := []byte(`{"cwd":"` + worktree + `","toolCall":{"name":"ask_question","args":{"questions":[{"question":"Which auth provider?","options":["Google","GitHub"]}]}}}`)

	if err := OnAsk(db, home, agyPayload); err != nil {
		t.Fatalf("OnAsk failed: %v", err)
	}

	gotTask, err := store.GetTask(db, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTask.Status != store.StatusNeedsYou {
		t.Fatalf("expected task status needs_you, got %s", gotTask.Status)
	}

	comments, err := store.ListComments(db, tk.ID)
	if err != nil || len(comments) == 0 {
		t.Fatalf("expected comment posted, got %v", comments)
	}
	if !strings.Contains(comments[0].Body, "Which auth provider?") || !strings.Contains(comments[0].Body, "Google") {
		t.Fatalf("unexpected comment body: %s", comments[0].Body)
	}

	// Claude payload should post the comment but NOT transition to needs_you (handled by Notification hook).
	tk2, err := store.CreateTask(db, store.Task{Key: "CV-2", Title: "Test Claude", Status: store.StatusRunning}, []store.TaskRepo{{RepoID: r.ID, IsPrimary: true, Branch: "b2"}})
	if err != nil {
		t.Fatal(err)
	}
	worktree2 := filepath.Join(home, "workspaces", "CV-2", "repo")
	claudePayload := []byte(`{"cwd":"` + worktree2 + `","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Proceed?","options":[{"label":"yes"}]}]}}`)
	if err := OnAsk(db, home, claudePayload); err != nil {
		t.Fatalf("OnAsk failed for Claude: %v", err)
	}
	gotTask2, err := store.GetTask(db, tk2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTask2.Status != store.StatusRunning {
		t.Fatalf("expected Claude task status to remain running, got %s", gotTask2.Status)
	}
}
