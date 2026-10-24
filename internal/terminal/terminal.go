// Package terminal opens a command in a terminal through a user template (tech-spec §10).
package terminal

import (
	"fmt"
	"os/exec"
	"strings"
)

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// layer makes s safe inside an AppleScript "..." string that itself sits inside a
// single-quoted sh argument: escape \ and ", then the sh single quote.
func layer(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	return strings.ReplaceAll(s, "'", `'\''`)
}

// Render fills {cwd} (shell-quoted) and {cmd} in template. Templates that call osascript get
// the extra AppleScript-safe escaping. An empty cmd becomes ":" so "cd {cwd} && {cmd}" stays valid.
func Render(template, cwd, cmd string) string {
	if cmd == "" {
		cmd = ":"
	}
	q := shq(cwd)
	if strings.Contains(template, "osascript") {
		q, cmd = layer(q), layer(cmd)
	}
	return strings.NewReplacer("{cwd}", q, "{cmd}", cmd).Replace(template)
}

// Open renders the template and runs it via `sh -c`.
func Open(template, cwd, cmd string) error {
	if out, err := exec.Command("sh", "-c", Render(template, cwd, cmd)).CombinedOutput(); err != nil {
		return fmt.Errorf("open terminal: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
