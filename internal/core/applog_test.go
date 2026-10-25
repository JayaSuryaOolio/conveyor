package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadAppLog(t *testing.T) {
	home := t.TempDir()
	if l, err := ReadAppLog(home, "app", -1); err != nil || l.Text != "" {
		t.Fatalf("missing log: %+v %v", l, err)
	}
	os.WriteFile(LogPath(home), []byte(strings.Repeat("x", logTail)+"one\n"), 0o644)
	l, err := ReadAppLog(home, "app", -1)
	if err != nil || len(l.Text) != logTail || !strings.HasSuffix(l.Text, "one\n") {
		t.Fatalf("tail: %d %v", len(l.Text), err)
	}
	f, _ := os.OpenFile(LogPath(home), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("two\n")
	f.Close()
	if l, _ = ReadAppLog(home, "app", l.Next); l.Text != "two\n" {
		t.Fatalf("follow: %q", l.Text)
	}
	os.WriteFile(LogPath(home), []byte("new\n"), 0o644) // rotated
	if l, _ = ReadAppLog(home, "app", l.Next); l.Text != "new\n" {
		t.Fatalf("rotated: %q", l.Text)
	}
}

func TestLogSources(t *testing.T) {
	home := t.TempDir()
	for src, want := range map[string]string{"app": "conveyor.log", "dispatcher": "logs/dispatcher.log", "hooks": "logs/hook.log"} {
		if p, ok := SourcePath(home, src); !ok || p != filepath.Join(home, want) {
			t.Fatalf("%s: %q %v", src, p, ok)
		}
		if l, err := ReadAppLog(home, src, -1); err != nil || l.Text != "" { // file missing
			t.Fatalf("%s missing: %+v %v", src, l, err)
		}
	}
	os.MkdirAll(filepath.Join(home, "logs"), 0o755)
	os.WriteFile(filepath.Join(home, "logs", "hook.log"), []byte("boom\n"), 0o644)
	if l, _ := ReadAppLog(home, "hooks", -1); l.Text != "boom\n" {
		t.Fatalf("hooks: %q", l.Text)
	}
	if _, err := ReadAppLog(home, "../etc/passwd", -1); err == nil {
		t.Fatal("free path accepted")
	}
}

// OpenLog must create logs/ (nothing else does) and rotate an oversized file.
func TestOpenLogCreatesDirAndRotates(t *testing.T) {
	home := t.TempDir()
	p := DispatcherLogPath(home)
	f, err := OpenLog(p)
	if err != nil {
		t.Fatalf("missing logs dir: %v", err)
	}
	f.Close()
	os.WriteFile(p, make([]byte, maxLog+1), 0o644)
	if f, err = OpenLog(p); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if fi, _ := os.Stat(p); fi.Size() != 0 {
		t.Fatalf("log not rotated: %d bytes", fi.Size())
	}
	if fi, err := os.Stat(p + ".1"); err != nil || fi.Size() != maxLog+1 {
		t.Fatalf("old log not kept: %v", err)
	}
	if sp, _ := SourcePath(home, "dispatcher"); sp != p {
		t.Fatalf("dispatcher source %s, writer %s", sp, p)
	}
	if sp, _ := SourcePath(home, "hooks"); sp != HookLogPath(home) {
		t.Fatalf("hooks source %s, writer %s", sp, HookLogPath(home))
	}
}
