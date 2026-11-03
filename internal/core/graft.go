package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var graftLine = regexp.MustCompile(`^(.+)  <-  (.+)  \(pid \d+\)$`)

// graftBin finds the graft CLI (the GUI's PATH often lacks ~/.local/bin).
func graftBin() string {
	if p, err := exec.LookPath("graft"); err == nil {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "bin", "graft")
}

// graftRoot returns the root that `graft list` output shows worktree mirrored into, "" if none.
func graftRoot(list, worktree string) string {
	for _, l := range strings.Split(list, "\n") {
		if m := graftLine.FindStringSubmatch(l); m != nil && m[2] == worktree {
			return m[1]
		}
	}
	return ""
}

// GraftRoot returns the root graft mirrors worktree into, "" when it is not active for it.
func GraftRoot(worktree string) string {
	out, err := exec.Command(graftBin(), "list").Output()
	if err != nil {
		return ""
	}
	return graftRoot(string(out), worktree)
}

// GraftStop stops the graft instance mirroring into root.
func GraftStop(root string) error {
	return runShell("stop graft", shq(graftBin())+" stop "+shq(root))
}
