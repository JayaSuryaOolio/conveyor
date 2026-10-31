package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergePATH(t *testing.T) {
	if got, want := mergePATH("/usr/bin:/bin", "/opt/x:/usr/bin::/home/u/.local/bin"), "/usr/bin:/bin:/opt/x:/home/u/.local/bin"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLoadShellPATH(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.Mkdir(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o755)
	// fake login shell: prints rc noise, then the marker with a PATH holding claude
	sh := filepath.Join(dir, "fakesh")
	os.WriteFile(sh, []byte("#!/bin/sh\necho noise\nprintf '__CONVEYOR_PATH__%s:/usr/bin__CONVEYOR_PATH__' '"+bin+"'\n"), 0o755)
	t.Setenv("SHELL", sh)
	t.Setenv("PATH", "/usr/bin:/bin")
	if _, err := exec.LookPath("claude"); err == nil {
		t.Skip("claude already on the minimal PATH")
	}
	loadShellPATH()
	if p, err := exec.LookPath("claude"); err != nil || !strings.HasPrefix(p, bin) {
		t.Fatalf("claude not found after loadShellPATH: %v %q", err, os.Getenv("PATH"))
	}
}
