package gh

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

const (
	checkTimeout = 30 * time.Second
	firstCheck   = 10 * time.Minute // after a PR is opened or pushed to
	agentPrefix  = "ai agent:"
)

// backoff is the delay before the next check after one that found nothing; then stop.
var backoff = []time.Duration{30 * time.Minute, 2 * time.Hour, 8 * time.Hour}

// ScheduleCheck replaces the task's pending review check with one at now+delay.
// step is the number of empty checks so far, so the next empty one uses backoff[step].
func ScheduleCheck(db *sql.DB, t store.Task, delay time.Duration, step int) error {
	arg, _ := json.Marshal(map[string]any{"args": []string{"epic", "review-check", t.Key, "--step", strconv.Itoa(step)}})
	return store.ScheduleOnce(db, store.Automation{
		Name: "review-check:" + t.Key, ActionKind: "cli", ActionArg: string(arg), TaskID: &t.ID,
	}, time.Now().Add(delay))
}

type user struct{ Login string }

type review struct {
	ID    int64
	Body  string
	State string
	User  user
}

type comment struct {
	ID   int64
	Body string
	Path string
	Line int
	User user
}

// parsePR splits https://github.com/o/r/pull/N.
func parsePR(url string) (owner, repo string, n int, err error) {
	p := strings.Split(strings.TrimSuffix(url, "/"), "/")
	if len(p) < 5 || p[len(p)-2] != "pull" {
		return "", "", 0, fmt.Errorf("not a PR url: %q", url)
	}
	n, err = strconv.Atoi(p[len(p)-1])
	return p[len(p)-4], p[len(p)-3], n, err
}

func api(ctx context.Context, dir string, args ...string) ([]byte, error) {
	bin, err := exec.LookPath("gh")
	if err != nil {
		return nil, ErrNotFound
	}
	cmd := exec.CommandContext(ctx, bin, append([]string{"api"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return nil, fmt.Errorf("gh api %s: %w: %s", args[0], err, ee.Stderr)
	}
	return out, err
}

const threadsQuery = `query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){pullRequest(number:$n){
reviewThreads(first:100){nodes{isResolved comments(first:100){nodes{databaseId}}}}}}}`

// ReviewCheck looks for review feedback on the task's open PRs that is newer than the watermark,
// not from an agent and not on a resolved thread. Anything found becomes one system comment that
// requeues the task; nothing found schedules the next backoff step. step is empty checks so far.
func ReviewCheck(ctx context.Context, svc *core.Service, key string, step int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	t, err := store.GetTaskByKey(svc.DB, key)
	if err != nil {
		return "", err
	}
	repos, err := store.ListRepos(svc.DB)
	if err != nil {
		return "", err
	}
	paths := map[int64]string{}
	for _, r := range repos {
		paths[r.ID] = r.Path
	}
	var body strings.Builder
	var errs []error
	open := 0
	for _, tr := range t.Repos {
		if tr.PRURL == nil || tr.PRState == nil || *tr.PRState != "OPEN" {
			continue
		}
		open++
		text, err := checkPR(ctx, svc.DB, t.ID, tr.RepoID, paths[tr.RepoID], *tr.PRURL)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", *tr.PRURL, err))
		}
		body.WriteString(text)
	}
	if err := errors.Join(errs...); err != nil {
		return "", err // watermark untouched, the next trigger retries
	}
	switch {
	case body.Len() > 0:
		_, err := svc.AddComment(key, store.AuthorSystem, strings.TrimSpace(body.String()), core.FollowUp)
		return "new review feedback", err
	case open > 0 && step < len(backoff):
		return "nothing new", ScheduleCheck(svc.DB, t, backoff[step], step+1)
	}
	return "nothing new", nil
}

// checkPR returns the new feedback text for one PR and advances its watermark.
func checkPR(ctx context.Context, db *sql.DB, taskID, repoID int64, dir, url string) (string, error) {
	owner, repo, n, err := parsePR(url)
	if err != nil {
		return "", err
	}
	base := fmt.Sprintf("repos/%s/%s/pulls/%d", owner, repo, n)
	// ponytail: first 100 only; add pagination if a PR outgrows it.
	var reviews []review
	var comments []comment
	var threads struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved bool
							Comments   struct{ Nodes []struct{ DatabaseID int64 } }
						}
					}
				}
			}
		}
	}
	for _, q := range []struct {
		dst  any
		args []string
	}{
		{&reviews, []string{base + "/reviews?per_page=100"}},
		{&comments, []string{base + "/comments?per_page=100"}},
		{&threads, []string{"graphql", "-f", "query=" + threadsQuery, "-F", "o=" + owner, "-F", "r=" + repo, "-F", "n=" + strconv.Itoa(n)}},
	} {
		out, err := api(ctx, dir, q.args...)
		if err != nil {
			return "", err
		}
		if err := json.Unmarshal(out, q.dst); err != nil {
			return "", fmt.Errorf("parse gh api %s: %w", q.args[0], err)
		}
	}
	resolved, unresolved := map[int64]bool{}, 0
	for _, th := range threads.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if !th.IsResolved {
			unresolved++
		}
		for _, c := range th.Comments.Nodes {
			resolved[c.DatabaseID] = th.IsResolved
		}
	}
	st, err := store.GetPRReview(db, taskID, repoID)
	if err != nil {
		return "", err
	}
	// ponytail: one watermark over review and comment ids; split them if GitHub ids stop being roughly monotonic.
	mark := st.SeenCommentID
	var sb strings.Builder
	keep := func(id int64, text string) bool {
		mark = max(mark, id)
		return id > st.SeenCommentID && strings.TrimSpace(text) != "" && !strings.HasPrefix(strings.TrimSpace(text), agentPrefix)
	}
	for _, r := range reviews {
		st.Review = r.State
		if keep(r.ID, r.Body) {
			fmt.Fprintf(&sb, "> review (%s) by %s\n%s\n\n", r.State, r.User.Login, r.Body)
		}
	}
	for _, c := range comments {
		if keep(c.ID, c.Body) && !resolved[c.ID] {
			fmt.Fprintf(&sb, "> %s:%d\n%s\n\n", c.Path, c.Line, c.Body)
		}
	}
	st.SeenCommentID, st.Unresolved = mark, unresolved
	if err := store.SavePRReview(db, st); err != nil {
		return "", err
	}
	if sb.Len() == 0 {
		return "", nil
	}
	return fmt.Sprintf("PR review on %s:\n%s", url, sb.String()), nil
}
