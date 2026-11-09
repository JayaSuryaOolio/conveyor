package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestDetectCommand(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir + "/c.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var buf bytes.Buffer
	oldOut := out
	out = &buf
	defer func() { out = oldOut }()

	if err := runDetect(dir, nil); err != nil {
		t.Fatal(err)
	}

	res := buf.String()
	if !strings.Contains(res, "DETECTED AGENT RUNNERS:") {
		t.Errorf("output missing header: %s", res)
	}
	if !strings.Contains(res, "Current default_runner:") {
		t.Errorf("output missing current runner: %s", res)
	}
}
