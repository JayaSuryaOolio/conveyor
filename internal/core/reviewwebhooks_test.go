package core

import "testing"

func TestReviewWebhooksInstall(t *testing.T) {
	s := newSvc(t)
	if s.ReviewWebhooksInstalled() {
		t.Fatal("installed on a fresh home")
	}
	if err := s.AddReviewWebhooks(); err != nil {
		t.Fatal(err)
	}
	if !s.ReviewWebhooksInstalled() {
		t.Fatal("not installed")
	}
	if p, _ := s.PlacedActions("global_nav"); len(p) != 1 || !p[0].Service {
		t.Fatalf("want one global_nav service button, got %+v", p)
	}
}
