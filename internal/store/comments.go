package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

const commentCols = `id, task_id, author, body, source, run_id, consumed_by_run_id, created_at, edits, send_now`

func queryComments(db dbtx, where string, args ...any) ([]Comment, error) {
	rows, err := db.Query(`SELECT `+commentCols+` FROM comments WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("query comments: %w", err)
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		var c Comment
		var edits string
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Author, &c.Body, &c.Source, &c.RunID, &c.ConsumedByRunID, &c.CreatedAt, &edits, &c.SendNow); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		if edits != "" {
			if err := json.Unmarshal([]byte(edits), &c.Edits); err != nil {
				return nil, fmt.Errorf("comment %d edits: %w", c.ID, err)
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddComment inserts a comment.
func AddComment(db *sql.DB, c Comment) (Comment, error) {
	c.CreatedAt = now()
	res, err := db.Exec(`INSERT INTO comments (task_id, author, body, source, run_id, created_at) VALUES (?,?,?,?,?,?)`,
		c.TaskID, c.Author, c.Body, c.Source, c.RunID, c.CreatedAt)
	if err != nil {
		return Comment{}, fmt.Errorf("add comment: %w", err)
	}
	c.ID, _ = res.LastInsertId()
	return c, nil
}

// PostRunComment adds the run's agent comment, or updates it when the run already has one (a question
// asked mid-run, then each Stop), keeping the replaced body in edits. A run without an id always adds.
func PostRunComment(db *sql.DB, c Comment) (Comment, error) {
	if c.RunID == nil {
		return AddComment(db, c)
	}
	cs, err := queryComments(db, `id=(SELECT MAX(id) FROM comments WHERE run_id=? AND author=?)`, *c.RunID, c.Author)
	if err != nil {
		return Comment{}, err
	}
	if len(cs) == 0 {
		return AddComment(db, c)
	}
	old := cs[0]
	if old.Body == c.Body {
		return old, nil
	}
	old.Edits = append(old.Edits, CommentEdit{Body: old.Body, At: now()})
	old.Body = c.Body
	edits, _ := json.Marshal(old.Edits)
	if _, err := db.Exec(`UPDATE comments SET body=?, edits=? WHERE id=?`, old.Body, string(edits), old.ID); err != nil {
		return Comment{}, fmt.Errorf("update comment: %w", err)
	}
	return old, nil
}

// ListComments returns a task's thread oldest first.
func ListComments(db *sql.DB, taskID int64) ([]Comment, error) {
	return queryComments(db, `task_id=?`, taskID)
}

// UnconsumedComments returns human/system comments no run has received yet.
func UnconsumedComments(db *sql.DB, taskID int64) ([]Comment, error) {
	return queryComments(db, `task_id=? AND author IN ('human','system') AND consumed_by_run_id IS NULL`, taskID)
}

// SendQueuedNow flags the task's queued human comments so the dispatcher launches without waiting
// out the debounce. It returns how many were flagged; 0 means a run already received them.
func SendQueuedNow(db *sql.DB, taskID int64) (int64, error) {
	res, err := db.Exec(`UPDATE comments SET send_now=1 WHERE task_id=? AND author='human' AND consumed_by_run_id IS NULL`, taskID)
	if err != nil {
		return 0, fmt.Errorf("send now: %w", err)
	}
	return res.RowsAffected()
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

// UnconsumeComments releases comments a run consumed, so the next run receives them.
func UnconsumeComments(db *sql.DB, runID int64) error {
	if _, err := db.Exec(`UPDATE comments SET consumed_by_run_id=NULL WHERE consumed_by_run_id=?`, runID); err != nil {
		return fmt.Errorf("unconsume comments: %w", err)
	}
	return nil
}

// TakeQueuedComment deletes a human comment no run has received yet and returns
// its body, so the UI can move it back into the editor. ok is false when a run
// consumed it first (or it is not a human comment of that task).
func TakeQueuedComment(db *sql.DB, taskID, id int64) (body string, ok bool, err error) {
	err = db.QueryRow(`SELECT body FROM comments WHERE id=? AND task_id=? AND author='human' AND consumed_by_run_id IS NULL`, id, taskID).Scan(&body)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("take comment: %w", err)
	}
	// Re-check the condition: a run may consume it between the SELECT and here.
	res, err := db.Exec(`DELETE FROM comments WHERE id=? AND consumed_by_run_id IS NULL`, id)
	if err != nil {
		return "", false, fmt.Errorf("take comment: %w", err)
	}
	n, _ := res.RowsAffected()
	return body, n == 1, nil
}

// ReleaseIfNothingQueued undoes a comment's requeue after the comment was taken back: a todo task
// that has already run and has no human comment queued goes back to needs_you instead of resuming empty.
// Queued system notes (e.g. "session was interrupted") don't count: they ride along with the next run.
func ReleaseIfNothingQueued(db *sql.DB, taskID int64) error {
	t, err := GetTask(db, taskID)
	if err != nil || t.Status != StatusTodo {
		return err
	}
	q, err := UnconsumedComments(db, taskID)
	if err != nil {
		return err
	}
	for _, c := range q {
		if c.Author == AuthorHuman {
			return nil
		}
	}
	if _, err := LatestRun(db, taskID); err != nil { // never ran: todo is the right state
		return nil
	}
	_, err = UpdateTaskStatus(db, taskID, StatusNeedsYou, ActorHuman)
	return err
}

// LatestComments returns every task's newest comment, keyed by task id; tasks without comments are absent.
func LatestComments(db *sql.DB) (map[int64]Comment, error) {
	cs, err := queryComments(db, `id IN (SELECT MAX(id) FROM comments GROUP BY task_id)`)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]Comment, len(cs))
	for _, c := range cs {
		out[c.TaskID] = c
	}
	return out, nil
}
