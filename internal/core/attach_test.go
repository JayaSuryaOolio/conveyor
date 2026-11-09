package core

import (
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/runner"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestAttachCommand(t *testing.T) {
	bg, sess := "123", "sess-1"
	run := store.Run{BgID: &bg, SessionID: &sess}
	// A live Claude run attaches to its bg session; resuming it would start a second process on it.
	if got := attachCommand(runner.NewClaudeRunner(), run, "/s.json", nil); got != "claude attach '123'" {
		t.Errorf("claude: %s", got)
	}
	got := attachCommand(runner.NewAgyRunner(t.TempDir()), run, "/s.json", nil)
	if !strings.HasPrefix(got, "kill -TERM 123 ") || !strings.Contains(got, "--conversation 'sess-1'") {
		t.Errorf("agy: %s", got)
	}
}
