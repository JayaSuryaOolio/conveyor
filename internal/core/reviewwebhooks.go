package core

import (
	"github.com/JayaSuryaOolio/conveyor/apps"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

const (
	reviewWebhooksApp       = "Review webhooks"
	reviewWebhooksDismissed = "review_webhooks_dismissed"
)

// ReviewWebhooksOffer reports whether to offer the opt-in app: not installed and not dismissed.
func (s *Service) ReviewWebhooksOffer() bool {
	if _, err := store.GetApp(s.DB, reviewWebhooksApp); err == nil {
		return false
	}
	return store.GetSetting(s.DB, reviewWebhooksDismissed) != "1"
}

// AddReviewWebhooks installs the bundled manifest (the same path as importing an app).
func (s *Service) AddReviewWebhooks() error {
	_, err := s.InstallManifest(apps.ReviewWebhooks, false)
	return err
}

// DismissReviewWebhooks stops offering the app.
func (s *Service) DismissReviewWebhooks() error {
	return store.SetSetting(s.DB, reviewWebhooksDismissed, "1")
}
