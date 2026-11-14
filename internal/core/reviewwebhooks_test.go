package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestHasOpenEpic(t *testing.T) {
	s := newSvc(t)
	if s.HasOpenEpic() {
		t.Fatal("open epic on a fresh home")
	}
	tk, err := s.CreateTask(NewTaskInput{Title: "e", Repos: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	store.AddLabel(s.DB, tk.ID, "epic:x")
	if !s.HasOpenEpic() {
		t.Fatal("open epic task not seen")
	}
}

// An enabled app's autostart service is started when down; a disabled app is left alone.
func TestEnsureServices(t *testing.T) {
	s := newSvc(t)
	flag := filepath.Join(t.TempDir(), "up")
	raw := `{"version":2,"name":"Svc","actions":[{"id":"go","label":"Go","places":["global_nav"],"run":"touch ` + flag + `",
	 "services":{"global_nav":{"status":"test -e ` + flag + `","stop":"rm ` + flag + `","autostart":true}}}]}`
	if _, err := s.InstallManifest(raw, false); err != nil {
		t.Fatal(err)
	}
	store.SetConnectorDisabled(s.DB, "Svc", true)
	if err := s.EnsureServices(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(flag); err == nil {
		t.Fatal("disabled app was started")
	}
	store.SetConnectorDisabled(s.DB, "Svc", false)
	if err := s.EnsureServices(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(flag); err != nil {
		t.Fatal("autostart service not started")
	}
}

func TestInstallCheckGatesInstall(t *testing.T) {
	s := newSvc(t)
	t.Setenv("PATH", t.TempDir()) // no acli on PATH
	if err := s.InstallExample("acli"); err == nil || !strings.Contains(err.Error(), "sample run failed") {
		t.Fatalf("installed without acli: %v", err)
	}
	if _, err := store.GetConnector(s.DB, "Atlassian CLI (acli)"); err == nil {
		t.Fatal("failed install left an app behind")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "acli"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	if err := s.InstallExample("acli"); err != nil {
		t.Fatal(err)
	}
}

// The app is found by its manifest, whatever its name or id; disabled apps are skipped.
func TestReviewWebhookAppID(t *testing.T) {
	s := newSvc(t)
	if id := s.ReviewWebhookConnectorID(); id != "" {
		t.Fatalf("id %q on a fresh home", id)
	}
	raw := `{"version":2,"name":"Other","id":"rw-v2","actions":[{"id":"go","label":"Go","places":["task_sidebar"],"run":"conveyor review-webhook --app ${{ app.id }}"}]}`
	if _, err := s.InstallManifest(raw, false); err != nil {
		t.Fatal(err)
	}
	if id := s.ReviewWebhookConnectorID(); id != "rw-v2" {
		t.Fatalf("id %q, want rw-v2", id)
	}
	store.SetConnectorDisabled(s.DB, "Other", true)
	if id := s.ReviewWebhookConnectorID(); id != "" {
		t.Fatalf("disabled app found: %q", id)
	}
}
