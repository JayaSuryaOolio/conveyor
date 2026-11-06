package store

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"unicode"
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

// NormalizeLearning lowercases body and collapses every run of non-letter/digit characters to one space, so
// wording that differs only in case or punctuation compares equal.
func NormalizeLearning(body string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(body), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

// PrefixLen is how many characters of the normalized body "same prefix" compares.
const PrefixLen = 40

func learningPrefix(body string) string {
	r := []rune(NormalizeLearning(body))
	if len(r) > PrefixLen {
		r = r[:PrefixLen]
	}
	return string(r)
}

// InsertLearningIfNew adds a proposed learning unless the repo (repoID nil = personal) already has one with the
// same normalized body in any status (taskID/runID 0 store NULL), so a rejected one doesn't come back reworded in case or punctuation.
func InsertLearningIfNew(db *sql.DB, taskID, runID int64, repoID *int64, body, createdAt string) error {
	rows, err := db.Query(`SELECT body FROM learnings WHERE repo_id IS ?`, repoID)
	if err != nil {
		return fmt.Errorf("insert learning: %w", err)
	}
	defer rows.Close()
	want := NormalizeLearning(body)
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return fmt.Errorf("insert learning: %w", err)
		}
		if NormalizeLearning(b) == want {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close() // single-connection DB: release it before writing
	_, err = db.Exec(`INSERT INTO learnings (task_id, run_id, repo_id, body, status, created_at) VALUES (?,?,?,?,'proposed',?)`,
		sql.NullInt64{Int64: taskID, Valid: taskID != 0}, sql.NullInt64{Int64: runID, Valid: runID != 0}, repoID, body, createdAt)
	if err != nil {
		return fmt.Errorf("insert learning: %w", err)
	}
	return nil
}

// RejectSamePrefix rejects the learning id and every other proposed learning (any repo) whose normalized body
// starts with the same PrefixLen characters. Accepted and applied ones are left alone. It returns the count rejected.
func RejectSamePrefix(db *sql.DB, id int64) (int, error) {
	l, err := GetLearning(db, id)
	if err != nil {
		return 0, err
	}
	p := learningPrefix(l.Body)
	if p == "" {
		return 0, fmt.Errorf("learning %d has no text to match", id)
	}
	return rejectProposed(db, func(o Learning) bool { return o.ID == id || learningPrefix(o.Body) == p })
}

// JunkCategories are the learning classes docs/learnings-review.md says to bulk-reject. Only the ones a body
// match can detect are here; recaps and stale facts need a human.
var JunkCategories = map[string]*regexp.Regexp{
	"placeholder":  regexp.MustCompile(`^(none|n a|na|nothing|no learning|no learnings)$`),
	"personal-env": regexp.MustCompile(`\brtk\b|\bsed i\b`),
}

// RejectCategory rejects every proposed learning in the named JunkCategories entry and returns the count.
func RejectCategory(db *sql.DB, category string) (int, error) {
	re, ok := JunkCategories[category]
	if !ok {
		return 0, fmt.Errorf("unknown learning category %q", category)
	}
	return rejectProposed(db, func(l Learning) bool { return re.MatchString(NormalizeLearning(l.Body)) })
}

func rejectProposed(db *sql.DB, match func(Learning) bool) (int, error) {
	all, err := ListLearnings(db, LearningProposed)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, l := range all {
		if !match(l) {
			continue
		}
		l.Status = LearningRejected
		if err := UpdateLearning(db, l); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
