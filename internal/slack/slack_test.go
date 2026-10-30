package slack

import (
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/chat"
)

func TestMessageKeyRoundTrip(t *testing.T) {
	pr := "https://github.com/o/r/pull/9"
	m := message(chat.Msg{Key: "CV-7", Title: "Fix it", Status: "needs_you", Last: "which index?\nbtree?", PRs: []chat.Link{{Name: "r", URL: pr}}})
	if keyOf(m) != "CV-7" || !strings.Contains(m, "<"+pr+"|r PR>") || !strings.Contains(m, ">btree?") {
		t.Fatalf("bad message:\n%s", m)
	}
	if keyOf("hello") != "" {
		t.Fatal("key from non-task text")
	}
}

func TestUserTokenWinsAndOwnPostsIgnored(t *testing.T) {
	cfg := map[string]string{"slack_bot_token": "xoxb", "slack_user_token": "xoxp", "slack_app_token": "xapp", "slack_user_id": "U1"}
	s := &Slack{func(k string) string { return cfg[k] }}
	if s.token() != "xoxp" || !s.Ready() {
		t.Fatal("user token should win and satisfy Ready")
	}
	delete(cfg, "slack_user_token")
	delete(cfg, "slack_bot_token")
	if s.Ready() {
		t.Fatal("no token must not be ready")
	}
	mine.Store("1.1", true)
	s.event([]byte(`{"event":{"type":"message","user":"U1","text":"x","channel":"D1","ts":"1.1"}}`), func(chat.In) string {
		t.Fatal("own post handled as a command")
		return ""
	})
}
