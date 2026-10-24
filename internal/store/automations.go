package store

import (
	"database/sql"
	"fmt"
)

const autoCols = `id, name, schedule, precheck, action_kind, action_arg, enabled, last_run_at, last_result`

func scanAutomation(r interface{ Scan(...any) error }) (Automation, error) {
	var a Automation
	err := r.Scan(&a.ID, &a.Name, &a.Schedule, &a.Precheck, &a.ActionKind, &a.ActionArg, &a.Enabled, &a.LastRunAt, &a.LastResult)
	return a, err
}

// CreateAutomation inserts an automation. A nil LastRunAt is stamped with the
// creation time, so the schedule counts from now rather than firing at once.
func CreateAutomation(db *sql.DB, a Automation) (Automation, error) {
	if a.LastRunAt == nil {
		n := now()
		a.LastRunAt = &n
	}
	res, err := db.Exec(`INSERT INTO automations (name, schedule, precheck, action_kind, action_arg, enabled, last_run_at, last_result)
 VALUES (?,?,?,?,?,?,?,?)`, a.Name, a.Schedule, a.Precheck, a.ActionKind, a.ActionArg, a.Enabled, a.LastRunAt, a.LastResult)
	if err != nil {
		return Automation{}, fmt.Errorf("create automation %q: %w", a.Name, err)
	}
	a.ID, _ = res.LastInsertId()
	return a, nil
}

// ListAutomations returns all automations ordered by name.
func ListAutomations(db *sql.DB) ([]Automation, error) {
	rows, err := db.Query(`SELECT ` + autoCols + ` FROM automations ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list automations: %w", err)
	}
	defer rows.Close()
	var out []Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan automation: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAutomation returns the automation or a wrapped sql.ErrNoRows.
func GetAutomation(db *sql.DB, name string) (Automation, error) {
	a, err := scanAutomation(db.QueryRow(`SELECT `+autoCols+` FROM automations WHERE name=?`, name))
	if err != nil {
		return Automation{}, fmt.Errorf("automation %q: %w", name, err)
	}
	return a, nil
}

// UpdateAutomation writes every column except id and name (looked up by ID).
func UpdateAutomation(db *sql.DB, a Automation) error {
	res, err := db.Exec(`UPDATE automations SET schedule=?, precheck=?, action_kind=?, action_arg=?, enabled=?, last_run_at=?, last_result=? WHERE id=?`,
		a.Schedule, a.Precheck, a.ActionKind, a.ActionArg, a.Enabled, a.LastRunAt, a.LastResult, a.ID)
	if err != nil {
		return fmt.Errorf("update automation %q: %w", a.Name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("automation %q: %w", a.Name, sql.ErrNoRows)
	}
	return nil
}

// DeleteAutomation removes an automation by name.
func DeleteAutomation(db *sql.DB, name string) error {
	res, err := db.Exec(`DELETE FROM automations WHERE name=?`, name)
	if err != nil {
		return fmt.Errorf("delete automation %q: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("automation %q: %w", name, sql.ErrNoRows)
	}
	return nil
}
