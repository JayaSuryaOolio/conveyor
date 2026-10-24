package core

import (
	"os"
	"strings"
	"testing"
)

func TestReadAppLog(t *testing.T) {
	home := t.TempDir()
	if l, err := ReadAppLog(home, -1); err != nil || l.Text != "" {
		t.Fatalf("missing log: %+v %v", l, err)
	}
	os.WriteFile(LogPath(home), []byte(strings.Repeat("x", logTail)+"one\n"), 0o644)
	l, err := ReadAppLog(home, -1)
	if err != nil || len(l.Text) != logTail || !strings.HasSuffix(l.Text, "one\n") {
		t.Fatalf("tail: %d %v", len(l.Text), err)
	}
	f, _ := os.OpenFile(LogPath(home), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("two\n")
	f.Close()
	if l, _ = ReadAppLog(home, l.Next); l.Text != "two\n" {
		t.Fatalf("follow: %q", l.Text)
	}
	os.WriteFile(LogPath(home), []byte("new\n"), 0o644) // rotated
	if l, _ = ReadAppLog(home, l.Next); l.Text != "new\n" {
		t.Fatalf("rotated: %q", l.Text)
	}
}
