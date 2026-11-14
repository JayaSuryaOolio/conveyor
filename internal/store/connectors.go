package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Connector is an installed app: its plugin manifest JSON (see connectors.Manifest).
type Connector struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Manifest string `json:"manifest"`
	// Disabled keeps the app (and its manifest) installed but inactive everywhere.
	Disabled bool `json:"disabled"`
}

// CreateConnector inserts an app; names are unique.
func CreateConnector(db *sql.DB, a Connector) (Connector, error) {
	res, err := db.Exec(`INSERT INTO connectors (name, manifest, disabled) VALUES (?,?,?)`, a.Name, a.Manifest, a.Disabled)
	if err != nil {
		return Connector{}, fmt.Errorf("create connector %q: %w", a.Name, err)
	}
	a.ID, _ = res.LastInsertId()
	return a, nil
}

// ListConnectors returns all apps ordered by name.
func ListConnectors(db *sql.DB) ([]Connector, error) {
	rows, err := db.Query(`SELECT id, name, manifest, disabled FROM connectors ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list connectors: %w", err)
	}
	defer rows.Close()
	out := []Connector{}
	for rows.Next() {
		var a Connector
		if err := rows.Scan(&a.ID, &a.Name, &a.Manifest, &a.Disabled); err != nil {
			return nil, fmt.Errorf("scan connector: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetConnector returns the app or a wrapped sql.ErrNoRows.
func GetConnector(db *sql.DB, name string) (Connector, error) {
	var a Connector
	if err := db.QueryRow(`SELECT id, name, manifest, disabled FROM connectors WHERE name=?`, name).Scan(&a.ID, &a.Name, &a.Manifest, &a.Disabled); err != nil {
		return Connector{}, fmt.Errorf("connector %q: %w", name, err)
	}
	return a, nil
}

// DeleteConnector removes an app by name.
func DeleteConnector(db *sql.DB, name string) error {
	res, err := db.Exec(`DELETE FROM connectors WHERE name=?`, name)
	if err != nil {
		return fmt.Errorf("delete connector %q: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("connector %q: %w", name, sql.ErrNoRows)
	}
	return nil
}

// SetConnectorDisabled turns the named app off or on without touching its manifest.
func SetConnectorDisabled(db *sql.DB, name string, disabled bool) error {
	res, err := db.Exec(`UPDATE connectors SET disabled=? WHERE name=?`, disabled, name)
	if err != nil {
		return fmt.Errorf("disable connector %q: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("connector %q: %w", name, sql.ErrNoRows)
	}
	return nil
}

// GetConnectorSettings returns the values the user saved for the app's manifest settings (key -> value).
func GetConnectorSettings(db *sql.DB, name string) (map[string]string, error) {
	var raw string
	if err := db.QueryRow(`SELECT settings FROM connectors WHERE name=?`, name).Scan(&raw); err != nil {
		return nil, fmt.Errorf("connector %q: %w", name, err)
	}
	m := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("connector %q settings: %w", name, err)
	}
	return m, nil
}

// SetConnectorSettings replaces the app's saved setting values.
func SetConnectorSettings(db *sql.DB, name string, vals map[string]string) error {
	b, _ := json.Marshal(vals)
	res, err := db.Exec(`UPDATE connectors SET settings=? WHERE name=?`, string(b), name)
	if err != nil {
		return fmt.Errorf("save connector %q settings: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("connector %q: %w", name, sql.ErrNoRows)
	}
	return nil
}
