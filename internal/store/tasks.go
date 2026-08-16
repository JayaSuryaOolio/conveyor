package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Task mirrors a kanban_tasks row (PRD Section 7).
type Task struct {
	ID                   string
	Title                string
	Description          string
	Status               Status
	Mode                 string // "headless" | "headful"
	RemoteControlEnabled bool
	Source               string // "manual" | "jira"
	ExternalKey          string
	ExternalURL          string
	WorktreePath         string
	WTSWorktreeID        string
	WTSTerminalID        string
	ClaudeSessionID      string
	ClaimLock            string
	ClaimedAt            string
	ConsecutiveFailures  int
	LatestMessage        string
	CreatedAt            string
	UpdatedAt            string
}

// CreateTask inserts a new task, generating its id and timestamps. Status
// defaults to draft and mode to headless per the DDL if left zero-valued.
func CreateTask(db *sql.DB, t Task) (Task, error) {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Status == "" {
		t.Status = StatusDraft
	}
	if t.Mode == "" {
		t.Mode = "headless"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	t.CreatedAt = now
	t.UpdatedAt = now

	_, err := db.Exec(`
		INSERT INTO kanban_tasks (
			id, title, description, status, mode, remote_control_enabled,
			source, external_key, external_url, worktree_path,
			wts_worktree_id, wts_terminal_id, claude_session_id,
			claim_lock, claimed_at, consecutive_failures, latest_message,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Title, nullable(t.Description), t.Status, t.Mode, t.RemoteControlEnabled,
		t.Source, nullable(t.ExternalKey), nullable(t.ExternalURL), nullable(t.WorktreePath),
		nullable(t.WTSWorktreeID), nullable(t.WTSTerminalID), nullable(t.ClaudeSessionID),
		nullable(t.ClaimLock), nullable(t.ClaimedAt), t.ConsecutiveFailures, nullable(t.LatestMessage),
		t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return Task{}, fmt.Errorf("insert task: %w", err)
	}
	return t, nil
}

// GetTask fetches a single task by id.
func GetTask(db *sql.DB, id string) (Task, error) {
	row := db.QueryRow(`
		SELECT id, title, description, status, mode, remote_control_enabled,
			source, external_key, external_url, worktree_path,
			wts_worktree_id, wts_terminal_id, claude_session_id,
			claim_lock, claimed_at, consecutive_failures, latest_message,
			created_at, updated_at
		FROM kanban_tasks WHERE id = ?`, id)
	return scanTask(row)
}

// ListTasks returns every task ordered by creation time. Status-based
// filtering/grouping is a frontend/API concern, not the store's.
func ListTasks(db *sql.DB) ([]Task, error) {
	rows, err := db.Query(`
		SELECT id, title, description, status, mode, remote_control_enabled,
			source, external_key, external_url, worktree_path,
			wts_worktree_id, wts_terminal_id, claude_session_id,
			claim_lock, claimed_at, consecutive_failures, latest_message,
			created_at, updated_at
		FROM kanban_tasks ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// UpdateTaskStatus validates the transition (PRD Section 4 / status.go), then
// applies it with a guarded UPDATE so a concurrent status change loses the
// race cleanly instead of silently overwriting.
func UpdateTaskStatus(db *sql.DB, id string, newStatus Status, actor Actor) (Task, error) {
	current, err := GetTask(db, id)
	if err != nil {
		return Task{}, err
	}
	if err := ValidateTransition(current.Status, newStatus, actor); err != nil {
		return Task{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`UPDATE kanban_tasks SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		newStatus, now, id, current.Status,
	)
	if err != nil {
		return Task{}, fmt.Errorf("update task status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Task{}, fmt.Errorf("update task status: %w", err)
	}
	if n == 0 {
		return Task{}, fmt.Errorf("task %s status changed concurrently, retry", id)
	}
	current.Status = newStatus
	current.UpdatedAt = now
	return current, nil
}

// RecordEvent inserts a kanban_task_events row. Callers decide when an event
// is worth recording — not every entry in the event enum corresponds 1:1
// with a status change.
func RecordEvent(db *sql.DB, taskID, event, detail string) error {
	_, err := db.Exec(
		`INSERT INTO kanban_task_events (task_id, event, detail, created_at) VALUES (?, ?, ?, ?)`,
		taskID, event, nullable(detail), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (Task, error) {
	var t Task
	var description, externalKey, externalURL, worktreePath sql.NullString
	var wtsWorktreeID, wtsTerminalID, claudeSessionID sql.NullString
	var claimLock, claimedAt, latestMessage sql.NullString

	err := s.Scan(
		&t.ID, &t.Title, &description, &t.Status, &t.Mode, &t.RemoteControlEnabled,
		&t.Source, &externalKey, &externalURL, &worktreePath,
		&wtsWorktreeID, &wtsTerminalID, &claudeSessionID,
		&claimLock, &claimedAt, &t.ConsecutiveFailures, &latestMessage,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Task{}, err
		}
		return Task{}, fmt.Errorf("scan task: %w", err)
	}

	t.Description = description.String
	t.ExternalKey = externalKey.String
	t.ExternalURL = externalURL.String
	t.WorktreePath = worktreePath.String
	t.WTSWorktreeID = wtsWorktreeID.String
	t.WTSTerminalID = wtsTerminalID.String
	t.ClaudeSessionID = claudeSessionID.String
	t.ClaimLock = claimLock.String
	t.ClaimedAt = claimedAt.String
	t.LatestMessage = latestMessage.String
	return t, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
