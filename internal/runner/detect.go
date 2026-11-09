package runner

import (
	"database/sql"
	"os/exec"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// CLIAgent represents a detected CLI agent/runner on the system.
type CLIAgent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
}

// KnownRunners returns the list of all supported runner backends.
func KnownRunners() []CLIAgent {
	return []CLIAgent{
		{ID: "claude", Name: "Claude Code", Command: "claude"},
		{ID: "agy", Name: "Antigravity", Command: "agy"},
	}
}

// lookPath is swappable in tests.
var lookPath = exec.LookPath

// DetectRunners checks PATH for each known runner CLI and returns their status.
func DetectRunners() []CLIAgent {
	agents := KnownRunners()
	for i := range agents {
		if p, err := lookPath(agents[i].Command); err == nil {
			agents[i].Installed = true
			agents[i].Path = p
		}
	}
	return agents
}

// AutoConfigureDefaultRunner sets default_runner from the installed runner CLIs, only while it is
// unset (no stored row): a stored value, the user's or an earlier pick, is never rewritten. When the
// default (claude) is not installed but agy is, it stores agy.
// Returns the active runner, whether a change was made, and any error.
func AutoConfigureDefaultRunner(db *sql.DB) (string, bool, error) {
	cur := store.GetSetting(db, "default_runner")
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM settings WHERE key='default_runner'`).Scan(&n); err != nil {
		return cur, false, err
	}
	if n > 0 {
		return cur, false, nil
	}
	agents := DetectRunners()
	installed := map[string]string{}
	for _, a := range agents {
		if a.Installed {
			installed[a.ID] = a.Path
		}
	}

	// If the current runner is installed, keep it.
	if _, ok := installed[cur]; ok {
		return cur, false, nil
	}

	// Current runner is NOT installed or empty. If we detected an alternative runner, switch to it.
	// Prefer agy if installed, else claude if installed.
	var picked string
	for _, id := range []string{"agy", "claude"} {
		if _, ok := installed[id]; ok {
			picked = id
			break
		}
	}
	if picked == "" {
		// None installed; leave as is
		return cur, false, nil
	}

	if err := store.SetSetting(db, "default_runner", picked); err != nil {
		return cur, false, err
	}
	return picked, true, nil
}

// InstructionFileForRunner returns the primary context / instructions file for a runner.
func InstructionFileForRunner(r string) string {
	switch r {
	case "agy":
		return "AGENTS.md"
	default:
		return "CLAUDE.md"
	}
}
