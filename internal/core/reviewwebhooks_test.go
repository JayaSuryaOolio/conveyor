package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestReviewWebhooksInstall(t *testing.T) {
	s := newSvc(t)
	if s.ReviewWebhooksInstalled() {
		t.Fatal("installed on a fresh home")
	}
	if err := s.InstallExample("review-webhooks"); err != nil {
		t.Fatal(err)
	}
	if !s.ReviewWebhooksInstalled() {
		t.Fatal("not installed")
	}
	if p, _ := s.PlacedActions("global_nav"); len(p) != 1 || !p[0].Service {
		t.Fatalf("want one global_nav service button, got %+v", p)
	}
}

func TestReviewWebhooksWanted(t *testing.T) {
	s := newSvc(t)
	if s.ReviewWebhooksWanted() {
		t.Fatal("wanted on a fresh home")
	}
	tk, err := s.CreateTask(NewTaskInput{Title: "e", Repos: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	store.AddLabel(s.DB, tk.ID, "epic:x")
	if !s.ReviewWebhooksWanted() {
		t.Fatal("open epic task should want it")
	}
}

func TestDisabledAppIsInert(t *testing.T) {
	s := newSvc(t)
	if err := s.InstallExample("review-webhooks"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAppDisabled(s.DB, ReviewWebhooksApp, true); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.PlacedActions("global_nav"); len(p) != 0 || s.ReviewWebhooksInstalled() {
		t.Fatalf("disabled app still active: %+v", p)
	}
	if _, err := s.RunAppAction(ReviewWebhooksApp, "review-webhooks", "global_nav", ""); err == nil {
		t.Fatal("disabled action ran")
	}
	if err := store.SetAppDisabled(s.DB, ReviewWebhooksApp, false); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.PlacedActions("global_nav"); len(p) != 1 || !s.ReviewWebhooksInstalled() {
		t.Fatal("re-enabled app not active")
	}
}

func TestInstallCheckGatesInstall(t *testing.T) {
	s := newSvc(t)
	t.Setenv("PATH", t.TempDir()) // no acli on PATH
	if err := s.InstallExample("acli"); err == nil || !strings.Contains(err.Error(), "sample run failed") {
		t.Fatalf("installed without acli: %v", err)
	}
	if _, err := store.GetApp(s.DB, "Atlassian CLI (acli)"); err == nil {
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
