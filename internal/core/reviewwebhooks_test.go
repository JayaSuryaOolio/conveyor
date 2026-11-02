package core

import "testing"

func TestReviewWebhooksOffer(t *testing.T) {
	s := newSvc(t)
	if !s.ReviewWebhooksOffer() {
		t.Fatal("not offered on a fresh home")
	}
	if err := s.AddReviewWebhooks(); err != nil {
		t.Fatal(err)
	}
	if s.ReviewWebhooksOffer() {
		t.Fatal("offered after install")
	}
	if p, _ := s.PlacedActions("global_nav"); len(p) != 1 || !p[0].Service {
		t.Fatalf("want one global_nav service button, got %+v", p)
	}
	if err := s.UninstallApp("Review webhooks"); err != nil {
		t.Fatal(err)
	}
	if err := s.DismissReviewWebhooks(); err != nil {
		t.Fatal(err)
	}
	if s.ReviewWebhooksOffer() {
		t.Fatal("offered after dismiss")
	}
}
