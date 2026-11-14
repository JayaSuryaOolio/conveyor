package core

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/connectors"
)

var graftLine = regexp.MustCompile(`^(.+)  <-  (.+)  \(pid \d+\)$`)

// runShell runs cmd via `sh -c`, folding its output into the error.
func runShell(what, cmd string) error {
	if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", what, err, strings.TrimSpace(string(out)))
	}
	return nil
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
	out, err := exec.Command(connectors.GraftBin(), "list").Output()
	if err != nil {
		return ""
	}
	return graftRoot(string(out), worktree)
}

// GraftStop stops the graft instance mirroring into root.
func GraftStop(root string) error {
	return runShell("stop graft", shq(connectors.GraftBin())+" stop "+shq(root))
}
