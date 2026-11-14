package core

import (
	"slices"
	"strings"
	"testing"
)

const settingsManifest = `{"version":3,"name":"Cfg",
 "settings":[{"key":"greeting","type":"string","default":"hi"},{"key":"loud","type":"boolean","default":false},
  {"key":"n","type":"number","default":2},{"key":"mode","type":"enum","options":["a","b"],"default":"a"}],
 "actions":[{"id":"say","label":"Say","places":["global_nav"],"output":true,"run":"echo ${{ setting.greeting }} ${{ setting.loud }} ${{ setting.n }} ${{ setting.mode }}"}]}`

func TestAppSettings(t *testing.T) {
	s := newSvc(t)
	if _, err := s.InstallManifest(settingsManifest, false); err != nil {
		t.Fatal(err)
	}
	run := func() string {
		out, err := s.RunConnectorAction("Cfg", "say", "global_nav", "")
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	if got := run(); got != "hi false 2 a" {
		t.Fatalf("defaults: %q", got)
	}
	// values are saved per app, validated by type, and reach scripts shell-quoted
	if err := s.SaveConnectorSettings("Cfg", map[string]string{"greeting": "it's $HOME", "loud": "true", "n": "5", "mode": "b"}); err != nil {
		t.Fatal(err)
	}
	if got := run(); got != "it's $HOME true 5 b" {
		t.Fatalf("saved: %q", got)
	}
	for _, bad := range []map[string]string{{"loud": "yes"}, {"n": "x"}, {"mode": "c"}, {"nope": "1"}} {
		if err := s.SaveConnectorSettings("Cfg", bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	if got := run(); got != "it's $HOME true 5 b" {
		t.Fatalf("rejected save changed values: %q", got)
	}
}

func TestReviewWebhookEventsFollowSettings(t *testing.T) {
	s := newSvc(t)
	if got := s.EnabledReviewWebhookEvents("review-webhooks"); !slices.Equal(got, ReviewWebhookEvents) {
		t.Fatalf("not installed: %v", got)
	}
	if err := s.InstallExample("review-webhooks"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveConnectorSettings("Review webhooks", map[string]string{"issue_comment": "false"}); err != nil {
		t.Fatal(err)
	}
	if got := s.EnabledReviewWebhookEvents("review-webhooks"); !slices.Equal(got, ReviewWebhookEvents[:2]) {
		t.Fatalf("issue_comment off: %v", got)
	}
}
