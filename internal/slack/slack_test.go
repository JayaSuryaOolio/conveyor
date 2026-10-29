package slack

import (
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestMessageKeyRoundTrip(t *testing.T) {
	pr := "https://github.com/o/r/pull/9"
	tk := store.Task{Key: "CV-7", Title: "Fix it", Repos: []store.TaskRepo{{RepoName: "r", PRURL: &pr}}}
	m := message(tk, "needs_you", "which index?\nbtree?")
	if keyOf(m) != "CV-7" || !strings.Contains(m, "<"+pr+"|r PR>") || !strings.Contains(m, ">btree?") {
		t.Fatalf("bad message:\n%s", m)
	}
	if keyOf("hello") != "" {
		t.Fatal("key from non-task text")
	}
}

func TestToStatus(t *testing.T) {
	if m := toStatus.FindStringSubmatch("running -> needs_you (system)"); m == nil || m[1] != "needs_you" {
		t.Fatal("system transition not matched")
	}
	if toStatus.MatchString("needs_you -> todo (human)") {
		t.Fatal("human transition must not notify")
	}
}
