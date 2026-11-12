package store

import (
	"database/sql"
	"fmt"
)

// Loop states and turns.
const (
	LoopActive   = "active"
	LoopApproved = "approved"
	LoopStopped  = "stopped"
	TurnWorker   = "worker"
	TurnChecker  = "checker"
)

// Loop hands turns between a worker task and a checker task, bounded by MaxRounds.
// A round is one request to the checker.
type Loop struct {
	ID        int64  `json:"id"`
	WorkerID  int64  `json:"-"`
	CheckerID int64  `json:"-"`
	Worker    string `json:"worker"`  // task key
	Checker   string `json:"checker"` // task key
	Kind      string `json:"kind"`
	Prompt    string `json:"prompt"`
	MaxRounds int    `json:"max_rounds"`
	Round     int    `json:"round"`
	Turn      string `json:"turn"`
	State     string `json:"state"`
	Reason    string `json:"reason"`
	LastSHA   string `json:"last_sha"` // worker PR head sent to the checker in the last round
	UpdatedAt string `json:"updated_at"`
}

const loopSelect = `SELECT l.id, l.worker_id, l.checker_id, w.key, c.key, l.kind, l.prompt, l.max_rounds, l.round, l.turn, l.state, l.reason, l.last_sha, l.updated_at
 FROM loops l JOIN tasks w ON w.id=l.worker_id JOIN tasks c ON c.id=l.checker_id`

func scanLoops(db *sql.DB, where string, args ...any) ([]Loop, error) {
	rows, err := db.Query(loopSelect+" "+where+" ORDER BY l.id", args...)
	if err != nil {
		return nil, fmt.Errorf("list loops: %w", err)
	}
	defer rows.Close()
	out := []Loop{}
	for rows.Next() {
		var l Loop
		if err := rows.Scan(&l.ID, &l.WorkerID, &l.CheckerID, &l.Worker, &l.Checker, &l.Kind, &l.Prompt, &l.MaxRounds, &l.Round, &l.Turn, &l.State, &l.Reason, &l.LastSHA, &l.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan loop: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// TaskLoops returns the loops the task is in (either side); activeOnly drops finished ones.
func TaskLoops(db *sql.DB, taskID int64, activeOnly bool) ([]Loop, error) {
	where := `WHERE (l.worker_id=? OR l.checker_id=?)`
	if activeOnly {
		where += ` AND l.state='active'`
	}
	return scanLoops(db, where, taskID, taskID)
}

// ListLoops returns every loop.
func ListLoops(db *sql.DB) ([]Loop, error) { return scanLoops(db, "") }

// CreateLoop inserts l and returns it with its id.
func CreateLoop(db *sql.DB, l Loop) (Loop, error) {
	l.UpdatedAt = now()
	res, err := db.Exec(`INSERT INTO loops (worker_id, checker_id, kind, prompt, max_rounds, round, turn, state, reason, last_sha, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		l.WorkerID, l.CheckerID, l.Kind, l.Prompt, l.MaxRounds, l.Round, l.Turn, l.State, l.Reason, l.LastSHA, l.UpdatedAt)
	if err != nil {
		return l, fmt.Errorf("create loop: %w", err)
	}
	l.ID, err = res.LastInsertId()
	return l, err
}

// SaveLoop stores l's progress (round, turn, state, reason, last sha).
func SaveLoop(db *sql.DB, l Loop) error {
	_, err := db.Exec(`UPDATE loops SET round=?, turn=?, state=?, reason=?, last_sha=?, updated_at=? WHERE id=?`,
		l.Round, l.Turn, l.State, l.Reason, l.LastSHA, now(), l.ID)
	if err != nil {
		return fmt.Errorf("save loop: %w", err)
	}
	return nil
}

// DeleteLoops removes every loop between worker and checker; it reports how many.
func DeleteLoops(db *sql.DB, workerID, checkerID int64) (int64, error) {
	res, err := db.Exec(`DELETE FROM loops WHERE worker_id=? AND checker_id=?`, workerID, checkerID)
	if err != nil {
		return 0, fmt.Errorf("delete loops: %w", err)
	}
	return res.RowsAffected()
}

// LoopLabel is how a loop reads on the task page, e.g. "peer-review loop 2/3: checker's turn".
func LoopLabel(l Loop) string {
	s := fmt.Sprintf("%s loop %d/%d: ", l.Kind, l.Round, l.MaxRounds)
	switch l.State {
	case LoopActive:
		return s + l.Turn + "'s turn"
	case LoopApproved:
		return s + "approved"
	}
	return s + "stopped (" + l.Reason + ")"
}
