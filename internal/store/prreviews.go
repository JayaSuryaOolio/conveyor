package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// PRReview is the review-check state of one task PR.
type PRReview struct {
	TaskID, RepoID int64
	HeadSHA        string
	SeenCommentID  int64 // review watermark
	Unresolved     int
	Review         string
}

// GetPRReview returns the state, or a zero value (with the keys set) if none yet.
func GetPRReview(db *sql.DB, taskID, repoID int64) (PRReview, error) {
	p := PRReview{TaskID: taskID, RepoID: repoID}
	err := db.QueryRow(`SELECT head_sha, seen_comment_id, unresolved, review FROM pr_reviews WHERE task_id=? AND repo_id=?`,
		taskID, repoID).Scan(&p.HeadSHA, &p.SeenCommentID, &p.Unresolved, &p.Review)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("get pr review: %w", err)
	}
	return p, nil
}

// SavePRReview upserts the state.
func SavePRReview(db *sql.DB, p PRReview) error {
	_, err := db.Exec(`INSERT INTO pr_reviews (task_id, repo_id, head_sha, seen_comment_id, unresolved, review) VALUES (?,?,?,?,?,?)
 ON CONFLICT(task_id, repo_id) DO UPDATE SET head_sha=excluded.head_sha, seen_comment_id=excluded.seen_comment_id, unresolved=excluded.unresolved, review=excluded.review`,
		p.TaskID, p.RepoID, p.HeadSHA, p.SeenCommentID, p.Unresolved, p.Review)
	if err != nil {
		return fmt.Errorf("save pr review: %w", err)
	}
	return nil
}
