package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var pathRe = regexp.MustCompile(`__CONVEYOR_PATH__(.*?)__CONVEYOR_PATH__`)

// loadShellPATH adds the login shell's PATH entries to this process's PATH. An app opened from
// the Dock or Finder gets only /usr/bin:/bin:/usr/sbin:/sbin, so `claude`, `gh` and `conveyor`
// would not be found. It is a no-op when `claude` is already on PATH.
func loadShellPATH() {
	if _, err := exec.LookPath("claude"); err == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	// -i as well as -l: PATH is often set in .zshrc. The markers skip any noise the rc files print.
	out, err := exec.CommandContext(ctx, sh, "-ilc", `printf __CONVEYOR_PATH__%s__CONVEYOR_PATH__ "$PATH"`).Output()
	if err != nil {
		return
	}
	if m := pathRe.FindSubmatch(out); m != nil {
		os.Setenv("PATH", mergePATH(os.Getenv("PATH"), string(m[1])))
	}
}

// mergePATH returns cur followed by the entries of extra that cur lacks.
func mergePATH(cur, extra string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range append(filepath.SplitList(cur), filepath.SplitList(extra)...) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}
