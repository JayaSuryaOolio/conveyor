package store

import (
	"database/sql"
	"fmt"
)

// Learning statuses.
const (
	LearningProposed = "proposed"
	LearningAccepted = "accepted"
	LearningRejected = "rejected"
	LearningApplied  = "applied"
)

const learningCols = `id, task_id, run_id, repo_id, body, status, target, applied_task_id, created_at`

// ListLearnings returns learnings (oldest first), optionally filtered to the given statuses.
func ListLearnings(db *sql.DB, statuses ...string) ([]Learning, error) {
	q := `SELECT ` + learningCols + ` FROM learnings`
	var args []any
	if len(statuses) > 0 {
		q += ` WHERE status IN (` + placeholders(len(statuses)) + `)`
		for _, s := range statuses {
			args = append(args, s)
		}
	}
	rows, err := db.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list learnings: %w", err)
	}
	defer rows.Close()
	var out []Learning
	for rows.Next() {
		var l Learning
		if err := rows.Scan(&l.ID, &l.TaskID, &l.RunID, &l.RepoID, &l.Body, &l.Status, &l.Target, &l.AppliedTaskID, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan learning: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// UpdateLearning writes status, target, body and applied_task_id. It refuses to touch an applied learning.
func UpdateLearning(db *sql.DB, l Learning) error {
	res, err := db.Exec(`UPDATE learnings SET status=?, target=?, body=?, applied_task_id=? WHERE id=? AND status<>'applied'`,
		l.Status, l.Target, l.Body, l.AppliedTaskID, l.ID)
	if err != nil {
		return fmt.Errorf("update learning %d: %w", l.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("learning %d: missing or already applied", l.ID)
	}
	return nil
}

// GetLearning returns one learning or a wrapped sql.ErrNoRows.
func GetLearning(db *sql.DB, id int64) (Learning, error) {
	var l Learning
	err := db.QueryRow(`SELECT `+learningCols+` FROM learnings WHERE id=?`, id).
		Scan(&l.ID, &l.TaskID, &l.RunID, &l.RepoID, &l.Body, &l.Status, &l.Target, &l.AppliedTaskID, &l.CreatedAt)
	if err != nil {
		return Learning{}, fmt.Errorf("learning %d: %w", id, err)
	}
	return l, nil
}
