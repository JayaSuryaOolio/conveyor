package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// App is an installed app: its plugin manifest JSON (see apps.Manifest).
type App struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Manifest string `json:"manifest"`
	// Disabled keeps the app (and its manifest) installed but inactive everywhere.
	Disabled bool `json:"disabled"`
}

// CreateApp inserts an app; names are unique.
func CreateApp(db *sql.DB, a App) (App, error) {
	res, err := db.Exec(`INSERT INTO apps (name, manifest, disabled) VALUES (?,?,?)`, a.Name, a.Manifest, a.Disabled)
	if err != nil {
		return App{}, fmt.Errorf("create app %q: %w", a.Name, err)
	}
	a.ID, _ = res.LastInsertId()
	return a, nil
}

// ListApps returns all apps ordered by name.
func ListApps(db *sql.DB) ([]App, error) {
	rows, err := db.Query(`SELECT id, name, manifest, disabled FROM apps ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	defer rows.Close()
	out := []App{}
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.Name, &a.Manifest, &a.Disabled); err != nil {
			return nil, fmt.Errorf("scan app: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetApp returns the app or a wrapped sql.ErrNoRows.
func GetApp(db *sql.DB, name string) (App, error) {
	var a App
	if err := db.QueryRow(`SELECT id, name, manifest, disabled FROM apps WHERE name=?`, name).Scan(&a.ID, &a.Name, &a.Manifest, &a.Disabled); err != nil {
		return App{}, fmt.Errorf("app %q: %w", name, err)
	}
	return a, nil
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

// SetAppDisabled turns the named app off or on without touching its manifest.
func SetAppDisabled(db *sql.DB, name string, disabled bool) error {
	res, err := db.Exec(`UPDATE apps SET disabled=? WHERE name=?`, disabled, name)
	if err != nil {
		return fmt.Errorf("disable app %q: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("app %q: %w", name, sql.ErrNoRows)
	}
	return nil
}

// GetAppSettings returns the values the user saved for the app's manifest settings (key -> value).
func GetAppSettings(db *sql.DB, name string) (map[string]string, error) {
	var raw string
	if err := db.QueryRow(`SELECT settings FROM apps WHERE name=?`, name).Scan(&raw); err != nil {
		return nil, fmt.Errorf("app %q: %w", name, err)
	}
	m := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("app %q settings: %w", name, err)
	}
	return m, nil
}

// SetAppSettings replaces the app's saved setting values.
func SetAppSettings(db *sql.DB, name string, vals map[string]string) error {
	b, _ := json.Marshal(vals)
	res, err := db.Exec(`UPDATE apps SET settings=? WHERE name=?`, string(b), name)
	if err != nil {
		return fmt.Errorf("save app %q settings: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("app %q: %w", name, sql.ErrNoRows)
	}
	return nil
}
