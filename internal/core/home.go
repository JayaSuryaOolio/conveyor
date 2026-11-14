// Package core holds domain operations shared by the CLI and the app.
package core

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/JayaSuryaOolio/conveyor/internal/connectors"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
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

// OpenDB opens the database under home and converts legacy plain command apps to manifest apps.
func OpenDB(home string) (*sql.DB, error) {
	db, err := store.Open(DBPath(home))
	if err != nil {
		return nil, err
	}
	if err := connectors.MigratePlain(db, home); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate connectors: %w", err)
	}
	if err := migrateIconsDir(db, home); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// migrateIconsDir moves the pre-rebrand app-icons dir to connector-icons and repoints the stored
// manifests' iconPaths at it. Idempotent: it does nothing once connector-icons exists.
func migrateIconsDir(db *sql.DB, home string) error {
	oldDir, newDir := filepath.Join(home, "app-icons"), connectors.IconsDir(home)
	if _, err := os.Stat(oldDir); err != nil {
		return nil
	}
	if _, err := os.Stat(newDir); err == nil {
		return nil
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return fmt.Errorf("rename app-icons: %w", err)
	}
	if _, err := db.Exec(`UPDATE connectors SET manifest=replace(manifest, ?, ?)`, oldDir+"/", newDir+"/"); err != nil {
		return fmt.Errorf("repoint connector icons: %w", err)
	}
	return nil
}

// WorkspacesDir holds the per-task workspace folders under home.
func WorkspacesDir(home string) string { return filepath.Join(home, "workspaces") }
