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
	// Review watermarks, one per GitHub id range; -1 = not yet baselined.
	SeenReviewID  int64 // reviews
	SeenInlineID  int64 // inline diff comments
	SeenCommentID int64 // PR conversation (issue) comments
	Unresolved    int
	Review        string
	Reported      string // CI/conflict state last sent to the agent
}

// GetPRReview returns the state, or a zero value (with the keys set) if none yet.
func GetPRReview(db *sql.DB, taskID, repoID int64) (PRReview, error) {
	p := PRReview{TaskID: taskID, RepoID: repoID}
	err := db.QueryRow(`SELECT head_sha, seen_review_id, seen_inline_id, seen_comment_id, unresolved, review, reported FROM pr_reviews WHERE task_id=? AND repo_id=?`,
		taskID, repoID).Scan(&p.HeadSHA, &p.SeenReviewID, &p.SeenInlineID, &p.SeenCommentID, &p.Unresolved, &p.Review, &p.Reported)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("get pr review: %w", err)
	}
	return p, nil
}

// SavePRReview upserts the state.
func SavePRReview(db *sql.DB, p PRReview) error {
	_, err := db.Exec(`INSERT INTO pr_reviews (task_id, repo_id, head_sha, seen_review_id, seen_inline_id, seen_comment_id, unresolved, review, reported) VALUES (?,?,?,?,?,?,?,?,?)
 ON CONFLICT(task_id, repo_id) DO UPDATE SET head_sha=excluded.head_sha, seen_review_id=excluded.seen_review_id, seen_inline_id=excluded.seen_inline_id, seen_comment_id=excluded.seen_comment_id, unresolved=excluded.unresolved, review=excluded.review, reported=excluded.reported`,
		p.TaskID, p.RepoID, p.HeadSHA, p.SeenReviewID, p.SeenInlineID, p.SeenCommentID, p.Unresolved, p.Review, p.Reported)
	if err != nil {
		return fmt.Errorf("save pr review: %w", err)
	}
	return nil
}
