package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// TaskLabels returns a task's label names, sorted; never nil.
func TaskLabels(db dbtx, taskID int64) ([]string, error) {
	rows, err := db.Query(`SELECT l.name FROM task_labels tl JOIN labels l ON l.id=tl.label_id WHERE tl.task_id=? ORDER BY l.name`, taskID)
	if err != nil {
		return nil, fmt.Errorf("task labels: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan label: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// allTaskLabels maps every task id to its sorted label names.
func allTaskLabels(db dbtx) (map[int64][]string, error) {
	rows, err := db.Query(`SELECT tl.task_id, l.name FROM task_labels tl JOIN labels l ON l.id=tl.label_id ORDER BY l.name`)
	if err != nil {
		return nil, fmt.Errorf("all task labels: %w", err)
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan label: %w", err)
		}
		out[id] = append(out[id], n)
	}
	return out, rows.Err()
}

// ListLabels returns every known label name, sorted (used for the dropdown).
func ListLabels(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM labels ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan label: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// AddLabel creates the label if new (names are case-insensitive, trimmed) and puts it on the task. Adding twice is a no-op.
func AddLabel(db *sql.DB, taskID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("label name is empty")
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("add label: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO labels (name) VALUES (?)`, name); err != nil {
		return fmt.Errorf("add label: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO task_labels (task_id, label_id) SELECT ?, id FROM labels WHERE name=?`, taskID, name); err != nil {
		return fmt.Errorf("add label: %w", err)
	}
	return tx.Commit()
}

// RemoveLabel takes a label off a task. The label itself stays in the dropdown list.
func RemoveLabel(db *sql.DB, taskID int64, name string) error {
	if _, err := db.Exec(`DELETE FROM task_labels WHERE task_id=? AND label_id=(SELECT id FROM labels WHERE name=?)`, taskID, strings.TrimSpace(name)); err != nil {
		return fmt.Errorf("remove label: %w", err)
	}
	return nil
}
