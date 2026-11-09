package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
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

// statusDetail is the status_changed event detail: "from -> to (actor)". StatusEventsAfter parses it back.
const statusDetail = "%s -> %s (%s)"

// StatusEvent is a parsed status_changed event.
type StatusEvent struct {
	ID, TaskID int64
	To         Status
	Actor      Actor
	At         time.Time
}

// StatusEventsAfter returns status_changed events with id > after, oldest first.
func StatusEventsAfter(db *sql.DB, after int64) ([]StatusEvent, error) {
	rows, err := db.Query(`SELECT id, task_id, detail, created_at FROM events WHERE kind='status_changed' AND id>? ORDER BY id`, after)
	if err != nil {
		return nil, fmt.Errorf("status events: %w", err)
	}
	defer rows.Close()
	var out []StatusEvent
	for rows.Next() {
		var e StatusEvent
		var detail, at string
		if err := rows.Scan(&e.ID, &e.TaskID, &detail, &at); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.At, _ = time.Parse(time.RFC3339, at)
		_, rest, _ := strings.Cut(detail, " -> ")
		to, actor, _ := strings.Cut(rest, " (")
		e.To, e.Actor = Status(to), Actor(strings.TrimSuffix(actor, ")"))
		out = append(out, e)
	}
	return out, rows.Err()
}

// HasLaterStatusEvent reports whether the task changed status again after event id.
func HasLaterStatusEvent(db *sql.DB, taskID, id int64) (bool, error) {
	var later bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM events WHERE task_id=? AND kind='status_changed' AND id>?)`, taskID, id).Scan(&later)
	return later, err
}

// MaxEventID returns the newest event id, 0 when there are none.
func MaxEventID(db *sql.DB) (id int64, err error) {
	err = db.QueryRow(`SELECT COALESCE(MAX(id),0) FROM events`).Scan(&id)
	return
}
