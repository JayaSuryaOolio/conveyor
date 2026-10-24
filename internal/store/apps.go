package store

import (
	"database/sql"
	"fmt"
)

// App is an external app a worktree can be opened in. Command is a shell
// template; {cwd} becomes the shell-quoted worktree path and {key} the task key.
type App struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	// Global apps are not tied to a worktree: shown in the nav bar, not on task pages.
	Global bool `json:"global"`
	// Output apps are run to completion and their output is shown (a quick action), instead of opening something.
	Output bool `json:"output"`
}

// CreateApp inserts an app; names are unique.
func CreateApp(db *sql.DB, a App) (App, error) {
	res, err := db.Exec(`INSERT INTO apps (name, command, global, output) VALUES (?,?,?,?)`, a.Name, a.Command, a.Global, a.Output)
	if err != nil {
		return App{}, fmt.Errorf("create app %q: %w", a.Name, err)
	}
	a.ID, _ = res.LastInsertId()
	return a, nil
}

// ListApps returns all apps ordered by name.
func ListApps(db *sql.DB) ([]App, error) {
	rows, err := db.Query(`SELECT id, name, command, global, output FROM apps ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	defer rows.Close()
	out := []App{}
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.Name, &a.Command, &a.Global, &a.Output); err != nil {
			return nil, fmt.Errorf("scan app: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetApp returns the app or a wrapped sql.ErrNoRows.
func GetApp(db *sql.DB, name string) (App, error) {
	var a App
	if err := db.QueryRow(`SELECT id, name, command, global, output FROM apps WHERE name=?`, name).Scan(&a.ID, &a.Name, &a.Command, &a.Global, &a.Output); err != nil {
		return App{}, fmt.Errorf("app %q: %w", name, err)
	}
	return a, nil
}

// UpdateApp renames, changes the command and/or flags of the app with a.ID.
func UpdateApp(db *sql.DB, a App) error {
	res, err := db.Exec(`UPDATE apps SET name=?, command=?, global=?, output=? WHERE id=?`, a.Name, a.Command, a.Global, a.Output, a.ID)
	if err != nil {
		return fmt.Errorf("update app %q: %w", a.Name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("app %q: %w", a.Name, sql.ErrNoRows)
	}
	return nil
}

// DeleteApp removes an app by name.
func DeleteApp(db *sql.DB, name string) error {
	res, err := db.Exec(`DELETE FROM apps WHERE name=?`, name)
	if err != nil {
		return fmt.Errorf("delete app %q: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("app %q: %w", name, sql.ErrNoRows)
	}
	return nil
}
