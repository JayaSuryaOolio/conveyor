package store

import (
	"database/sql"
	"fmt"
)

const commentCols = `id, task_id, author, body, run_id, consumed_by_run_id, created_at`

func queryComments(db dbtx, where string, args ...any) ([]Comment, error) {
	rows, err := db.Query(`SELECT `+commentCols+` FROM comments WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("query comments: %w", err)
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Author, &c.Body, &c.RunID, &c.ConsumedByRunID, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddComment inserts a comment.
func AddComment(db *sql.DB, c Comment) (Comment, error) {
	c.CreatedAt = now()
	res, err := db.Exec(`INSERT INTO comments (task_id, author, body, run_id, created_at) VALUES (?,?,?,?,?)`,
		c.TaskID, c.Author, c.Body, c.RunID, c.CreatedAt)
	if err != nil {
		return Comment{}, fmt.Errorf("add comment: %w", err)
	}
	c.ID, _ = res.LastInsertId()
	return c, nil
}

// ListComments returns a task's thread oldest first.
func ListComments(db *sql.DB, taskID int64) ([]Comment, error) {
	return queryComments(db, `task_id=?`, taskID)
}

// UnconsumedComments returns human/system comments no run has received yet.
func UnconsumedComments(db *sql.DB, taskID int64) ([]Comment, error) {
	return queryComments(db, `task_id=? AND author IN ('human','system') AND consumed_by_run_id IS NULL`, taskID)
}

// ConsumeComments marks comments as received by a run.
func ConsumeComments(db *sql.DB, ids []int64, runID int64) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{runID}
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := db.Exec(`UPDATE comments SET consumed_by_run_id=? WHERE id IN (`+placeholders(len(ids))+`)`, args...); err != nil {
		return fmt.Errorf("consume comments: %w", err)
	}
	return nil
}
