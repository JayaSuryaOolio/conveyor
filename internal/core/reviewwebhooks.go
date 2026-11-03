package core

import (
	"github.com/JayaSuryaOolio/conveyor/apps"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// ReviewWebhooksApp is the bundled app's name; installing it (Apps screen) turns on event-driven review checks for epic tasks.
const ReviewWebhooksApp = "Review webhooks"

// ReviewWebhooksInstalled reports whether the app is installed.
func (s *Service) ReviewWebhooksInstalled() bool {
	_, err := store.GetApp(s.DB, ReviewWebhooksApp)
	return err == nil
}

// AddReviewWebhooks installs the bundled manifest (the same path as importing an app).
func (s *Service) AddReviewWebhooks() error {
	_, err := s.InstallManifest(apps.ReviewWebhooks, false)
	return err
}
