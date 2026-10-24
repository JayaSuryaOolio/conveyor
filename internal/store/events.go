package store

import (
	"database/sql"
	"fmt"
)

func recordEvent(db dbtx, taskID int64, kind, detail string) error {
	_, err := db.Exec(`INSERT INTO events (task_id, kind, detail, created_at) VALUES (?,?,?,?)`, taskID, kind, detail, now())
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	return nil
}

// RecordEvent appends an audit row for a task.
func RecordEvent(db *sql.DB, taskID int64, kind, detail string) error {
	return recordEvent(db, taskID, kind, detail)
}

// ListEvents returns a task's events oldest first.
func ListEvents(db *sql.DB, taskID int64) ([]Event, error) {
	rows, err := db.Query(`SELECT id, task_id, kind, detail, created_at FROM events WHERE task_id=? ORDER BY id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Kind, &e.Detail, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
