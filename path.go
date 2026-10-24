package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

const pathMarker = "__CONVEYOR_PATH__="

// useLoginShellPath replaces PATH with the user's interactive login shell PATH.
// An app launched from Finder gets launchd's minimal PATH and can't find claude, gh, git or conveyor.
func useLoginShellPath() {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// -i so rc files that set PATH run too; the marker skips anything they print.
	out, err := exec.CommandContext(ctx, sh, "-ilc", "printf '"+pathMarker+"%s' \"$PATH\"").Output()
	if p := shellPath(string(out)); err == nil && p != "" {
		os.Setenv("PATH", p)
	}
}

func shellPath(out string) string {
	if i := strings.LastIndex(out, pathMarker); i >= 0 {
		return strings.TrimSpace(out[i+len(pathMarker):])
	}
	return ""
}
