package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// Decision is a choice recorded on a task ("what — why"). Disputed decisions carry a one-line reason and who disputed.
type Decision struct {
	ID         int64  `json:"id"`
	TaskID     int64  `json:"task_id"`
	Body       string `json:"body"`
	Author     string `json:"author"`      // AuthorHuman | AuthorAgent
	DisputedBy string `json:"disputed_by"` // "" when not disputed
	Dispute    string `json:"dispute"`
	Kind       string `json:"kind"` // KindDecision | KindFinding
	CreatedAt  string `json:"created_at"`
}

// Decision kinds. A finding is a fact learned along the way (review, CI, test, human), not a choice.
const (
	KindDecision = "decision"
	KindFinding  = "finding"
)

// AddDecision records a decision unless the task already has the same body (agents repeat lines across runs).
func AddDecision(db *sql.DB, taskID int64, author, body string) (int64, error) {
	return AddEntry(db, taskID, author, KindDecision, body)
}

// AddEntry records a decision or finding (kind) with the same repeat check.
func AddEntry(db *sql.DB, taskID int64, author, kind, body string) (int64, error) {
	body = strings.Join(strings.Fields(body), " ")
	if body == "" {
		return 0, fmt.Errorf("decision is empty")
	}
	var id int64
	if err := db.QueryRow(`SELECT id FROM decisions WHERE task_id=? AND body=?`, taskID, body).Scan(&id); err == nil {
		return id, nil
	}
	res, err := db.Exec(`INSERT INTO decisions(task_id, body, author, kind) VALUES(?,?,?,?)`, taskID, body, author, kind)
	if err != nil {
		return 0, fmt.Errorf("add decision: %w", err)
	}
	return res.LastInsertId()
}

// ListDecisions returns the task's decisions, oldest first.
func ListDecisions(db *sql.DB, taskID int64) ([]Decision, error) {
	rows, err := db.Query(`SELECT id, task_id, body, author, disputed_by, dispute, kind, created_at FROM decisions WHERE task_id=? ORDER BY id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list decisions: %w", err)
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.ID, &d.TaskID, &d.Body, &d.Author, &d.DisputedBy, &d.Dispute, &d.Kind, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan decision: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func decisionExec(db *sql.DB, taskID, id int64, q string, args ...any) error {
	res, err := db.Exec(q, append(args, id, taskID)...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("decision %d: not on this task", id)
	}
	return nil
}

// RemoveDecision strikes a decision from the task.
func RemoveDecision(db *sql.DB, taskID, id int64) error {
	return decisionExec(db, taskID, id, `DELETE FROM decisions WHERE id=? AND task_id=?`)
}

// SetDispute marks a decision disputed by who (AuthorHuman | AuthorAgent) with a one-line reason.
func SetDispute(db *sql.DB, taskID, id int64, by, reason string) error {
	reason = strings.Join(strings.Fields(reason), " ")
	if reason == "" {
		return fmt.Errorf("a dispute needs a one-line reason")
	}
	return decisionExec(db, taskID, id, `UPDATE decisions SET disputed_by=?, dispute=? WHERE id=? AND task_id=?`, by, reason)
}

// ClearDispute unmarks a disputed decision.
func ClearDispute(db *sql.DB, taskID, id int64) error {
	return decisionExec(db, taskID, id, `UPDATE decisions SET disputed_by='', dispute='' WHERE id=? AND task_id=?`)
}
