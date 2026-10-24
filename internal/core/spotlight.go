package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var spotlightLine = regexp.MustCompile(`^(.+)  <-  (.+)  \(pid \d+\)$`)

// spotlightBin finds the spotlight CLI (the GUI's PATH often lacks ~/.local/bin).
func spotlightBin() string {
	if p, err := exec.LookPath("spotlight"); err == nil {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "bin", "spotlight")
}

// spotlightRoot returns the root that `spotlight list` output shows worktree mirrored into, "" if none.
func spotlightRoot(list, worktree string) string {
	for _, l := range strings.Split(list, "\n") {
		if m := spotlightLine.FindStringSubmatch(l); m != nil && m[2] == worktree {
			return m[1]
		}
	}
	return ""
}

// SpotlightRoot returns the root spotlight mirrors worktree into, "" when it is not active for it.
func SpotlightRoot(worktree string) string {
	out, err := exec.Command(spotlightBin(), "list").Output()
	if err != nil {
		return ""
	}
	return spotlightRoot(string(out), worktree)
}

// SpotlightStop stops the spotlight instance mirroring into root.
func SpotlightStop(root string) error {
	return runShell("stop spotlight", shq(spotlightBin())+" stop "+shq(root))
}
