package core

import (
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
