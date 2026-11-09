// Package gh tracks pull requests through the gh CLI.
package gh

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// ErrNotFound means the gh CLI is not on PATH.
var ErrNotFound = errors.New("gh CLI not found")

// PR is the subset of `gh pr list` output Conveyor tracks.
type PR struct {
	URL            string `json:"url"`
	State          string `json:"state"` // OPEN, MERGED or CLOSED
	Number         int    `json:"number"`
	ReviewDecision string `json:"reviewDecision"`
	UpdatedAt      string `json:"updatedAt"`
	HeadRefOid     string `json:"headRefOid"`
}

// ghBin resolves the gh CLI, or ErrNotFound.
func ghBin() (string, error) {
	bin, err := exec.LookPath("gh")
	if err != nil {
		return "", ErrNotFound
	}
	return bin, nil
}

// run execs gh args in dir; a failure is prefixed with name and carries gh's stderr.
func run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	bin, err := ghBin()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("%s: %w: %s", name, err, ee.Stderr)
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// FindPR returns the latest PR for branch in repoPath, or (nil, nil) if none.
func FindPR(ctx context.Context, repoPath, branch string) (*PR, error) {
	out, err := run(ctx, repoPath, "gh pr list "+branch, "pr", "list", "--head", branch, "--state", "all",
		"--json", "url,state,number,reviewDecision,updatedAt,headRefOid", "--limit", "1")
	if err != nil {
		return nil, err
	}
	var prs []PR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

// SyncTask updates pr_url and pr_state for every task repo, then moves a task
// in review to done (all PRs merged or closed, at least one merged) or to
// needs_you (every PR closed without merge).
func SyncTask(ctx context.Context, db *sql.DB, task store.Task) error {
	paths, err := store.RepoMap(db, func(r store.Repo) string { return r.Path })
	if err != nil {
		return err
	}
	var total, merged, closed int
	for _, tr := range task.Repos {
		pr, err := FindPR(ctx, paths[tr.RepoID], tr.Branch)
		if err != nil {
			return err
		}
		if pr == nil {
			continue
		}
		tr.PRURL, tr.PRState = &pr.URL, &pr.State
		if err := store.UpdateTaskRepo(db, tr); err != nil {
			return err
		}
		if err := scheduleOnNewHead(db, task, tr.RepoID, pr); err != nil {
			return err
		}
		total++
		switch pr.State {
		case "MERGED":
			merged++
		case "CLOSED":
			closed++
		}
	}
	if task.Status != store.StatusReview || total == 0 {
		return nil
	}
	switch {
	case merged > 0 && merged+closed == total:
		if _, err := store.UpdateTaskStatus(db, task.ID, store.StatusDone, store.ActorSystem); err != nil {
			return err
		}
		return store.RecordEvent(db, task.ID, "pr_merged", fmt.Sprintf("%d of %d PRs merged", merged, total))
	case closed == total:
		if _, err := store.AddComment(db, store.Comment{TaskID: task.ID, Author: store.AuthorSystem, Body: "PR closed without merge"}); err != nil {
			return err
		}
		_, err := store.UpdateTaskStatus(db, task.ID, store.StatusNeedsYou, store.ActorSystem)
		return err
	}
	return nil
}

// scheduleOnNewHead schedules a review check 10 min out when an open PR is new or was pushed to.
func scheduleOnNewHead(db *sql.DB, t store.Task, repoID int64, pr *PR) error {
	if pr.State != "OPEN" || pr.HeadRefOid == "" {
		return nil
	}
	st, err := store.GetPRReview(db, t.ID, repoID)
	if err != nil || st.HeadSHA == pr.HeadRefOid {
		return err
	}
	st.HeadSHA = pr.HeadRefOid
	if err := store.SavePRReview(db, st); err != nil {
		return err
	}
	return ScheduleCheck(db, t, firstCheck, 0)
}
