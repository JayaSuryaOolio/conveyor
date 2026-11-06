package core

import (
	"slices"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// ReviewWebhookEvents are the GitHub events the listener can forward; an app turns each off with a
// boolean setting named after the event (see examples/review-webhooks.json).
var ReviewWebhookEvents = []string{"pull_request_review", "pull_request_review_comment", "issue_comment"}

// EnabledReviewWebhookEvents are the events the settings of the app with this id (Manifest.AppID)
// leave on; all of them when no such app is installed.
func (s *Service) EnabledReviewWebhookEvents(appID string) []string {
	off := map[string]bool{}
	apps, _ := store.ListApps(s.DB)
	for _, a := range apps {
		if m, err := ParseManifest(a.Manifest); err == nil && m.AppID() == appID {
			st, _ := s.appSettings(a.Name)
			for _, x := range st {
				off[x.Key] = x.Value == "false"
			}
		}
	}
	return slices.DeleteFunc(slices.Clone(ReviewWebhookEvents), func(e string) bool { return off[e] })
}
