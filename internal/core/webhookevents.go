package core

import (
	"slices"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/apps"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// ReviewWebhookEvents are the GitHub events the listener can forward; an app turns each off with a
// boolean setting named after the event (see examples/review-webhooks.json).
var ReviewWebhookEvents = []string{"pull_request_review", "pull_request_review_comment", "issue_comment"}

// EnabledReviewWebhookEvents are the events the settings of the app with this id (apps.Manifest.AppID)
// leave on; all of them when no such app is installed.
func (s *Service) EnabledReviewWebhookEvents(appID string) []string {
	off := map[string]bool{}
	list, _ := store.ListApps(s.DB)
	for _, a := range list {
		if m, err := apps.ParseManifest(a.Manifest); err == nil && m.AppID() == appID {
			st, _ := apps.AppSettings(s.DB, a.Name)
			for _, x := range st {
				off[x.Key] = x.Value == "false"
			}
		}
	}
	return slices.DeleteFunc(slices.Clone(ReviewWebhookEvents), func(e string) bool { return off[e] })
}

// ReviewWebhookAppID is the id of the first enabled app with an action whose run starts
// `conveyor review-webhook`, or "" when none is installed.
func (s *Service) ReviewWebhookAppID() string {
	list, _ := store.ListApps(s.DB)
	for _, a := range list {
		m, err := apps.ParseManifest(a.Manifest)
		if a.Manifest == "" || a.Disabled || err != nil {
			continue
		}
		for _, x := range m.Actions {
			if strings.Contains(x.Run, "conveyor review-webhook") {
				return m.AppID()
			}
		}
	}
	return ""
}
