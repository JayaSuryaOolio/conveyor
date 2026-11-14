package gh

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

const (
	rescanEvery = time.Minute
)

// ErrNoWebhookExt means `gh webhook` is not installed.
var ErrNoWebhookExt = errors.New("the gh webhook extension is missing: run `gh extension install cli/gh-webhook`")

// CheckWebhookExt errors with ErrNoWebhookExt (or ErrNotFound) when `gh webhook` is unusable.
func CheckWebhookExt() error {
	bin, err := ghBin()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if exec.CommandContext(ctx, bin, "webhook", "forward", "--help").Run() != nil {
		return ErrNoWebhookExt
	}
	return nil
}

// WebhookHandler turns a review event on a task's PR into a check scheduled for now.
// It binds loopback only and the effect is just that scheduling, so it carries no secret.
func WebhookHandler(db *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ev := r.Header.Get("X-GitHub-Event")
		if r.Method != http.MethodPost || (ev != "pull_request_review" && ev != "pull_request_review_comment" && ev != "issue_comment") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var p struct {
			PullRequest struct {
				URL string `json:"html_url"`
			} `json:"pull_request"`
			// Issue is the payload of issue_comment, whose html_url is the PR's when the comment is on a PR.
			Issue struct {
				URL string `json:"html_url"`
			} `json:"issue"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		prURL := p.PullRequest.URL
		if prURL == "" {
			prURL = p.Issue.URL
		}
		tasks, err := openTasks(db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, t := range tasks {
			for _, tr := range t.Repos {
				if tr.PRURL != nil && *tr.PRURL == prURL {
					if err := ScheduleCheck(db, t, 0, 0); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					log.Printf("review webhook: %s on %s, check scheduled", ev, t.Key)
					return
				}
			}
		}
		log.Printf("review webhook: %s on %s, no open task has that PR", ev, prURL)
		w.WriteHeader(http.StatusNoContent)
	})
}

// openTasks lists unfinished tasks.
func openTasks(db *sql.DB) ([]store.Task, error) {
	all, err := store.ListTasks(db)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(t store.Task) bool {
		return t.Status == store.StatusDone || t.Status == store.StatusCancelled
	}), nil
}

// openPRRepos lists owner/repo for every open PR of an unfinished task.
func openPRRepos(db *sql.DB) []string {
	tasks, err := openTasks(db)
	if err != nil {
		log.Printf("review webhook: list tasks: %v", err)
		return nil
	}
	var out []string
	for _, t := range tasks {
		for _, tr := range t.Repos {
			if tr.PRURL == nil || tr.PRState == nil || *tr.PRState != "OPEN" {
				continue
			}
			if o, r, _, err := parsePR(*tr.PRURL); err == nil && !slices.Contains(out, o+"/"+r) {
				out = append(out, o+"/"+r)
			}
		}
	}
	slices.Sort(out)
	return out
}

// RunWebhook serves the handler on a random loopback port and keeps one `gh webhook forward`
// per repo with an open PR until ctx ends. A failed forward (GitHub allows one per repo) is
// logged and retried on the next scan; the backoff checks still run. events are fixed until it restarts.
func RunWebhook(ctx context.Context, db *sql.DB, events []string) error {
	if len(events) == 0 {
		return errors.New("no review webhook events enabled: turn one on in the connector's settings")
	}
	if err := CheckWebhookExt(); err != nil {
		return err
	}
	bin, err := ghBin()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: WebhookHandler(db), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()
	url := "http://" + ln.Addr().String()
	log.Printf("review webhook: listening on %s", url)

	running := map[string]bool{}
	done := make(chan string)
	scan := func() {
		for _, repo := range openPRRepos(db) {
			if running[repo] {
				continue
			}
			running[repo] = true
			log.Printf("review webhook: forwarding %s", repo)
			cmd := exec.CommandContext(ctx, bin, "webhook", "forward", "--repo", repo, "--events="+strings.Join(events, ","), "--url", url)
			go func() {
				out, err := cmd.CombinedOutput()
				if ctx.Err() == nil {
					log.Printf("review webhook: forward %s stopped: %v: %s", repo, err, out)
				}
				done <- repo
			}()
		}
	}
	tick := time.NewTicker(rescanEvery)
	defer tick.Stop()
	scan()
	for {
		select {
		case <-ctx.Done():
			return nil
		case repo := <-done:
			delete(running, repo)
		case <-tick.C:
			scan()
		}
	}
}
