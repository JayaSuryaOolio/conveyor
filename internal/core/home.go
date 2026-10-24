// Package core holds domain operations shared by the CLI and the app.
package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// Home resolves CONVEYOR_HOME: the flag value, else $CONVEYOR_HOME, else
// ~/.conveyor. It creates the directory.
func Home(flagVal string) (string, error) {
	h := flagVal
	if h == "" {
		h = os.Getenv("CONVEYOR_HOME")
	}
	if h == "" {
		u, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home dir: %w", err)
		}
		h = filepath.Join(u, ".conveyor")
	}
	h, err := filepath.Abs(h)
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	if err := os.MkdirAll(h, 0o755); err != nil {
		return "", fmt.Errorf("create home: %w", err)
	}
	return h, nil
}

// DBPath is the SQLite file under home.
func DBPath(home string) string { return filepath.Join(home, "conveyor.db") }

// WorkspacesDir holds the per-task workspace folders under home.
func WorkspacesDir(home string) string { return filepath.Join(home, "workspaces") }
