package jira

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// defaultStatusMap is the Conveyor status -> Jira status name map; a source's status_map overrides entries.
var defaultStatusMap = map[store.Status]string{
	store.StatusBacklog: "To Do", store.StatusTodo: "To Do", store.StatusRunning: "In Progress",
	store.StatusReview: "In Review", store.StatusDone: "Done",
}

var (
	mu sync.Mutex // serialises write-backs so a status and a PR event never double-post
	wg sync.WaitGroup
)

// Attach writes task status moves and branch/PR changes back to Jira, asynchronously.
// Failures go to the source's last_error, never to the caller. It returns the detach func.
func Attach(svc *core.Service) (detach func()) {
	return store.OnTaskChange(func(id int64) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if epic, finished := writeBack(ctx, svc, id); finished {
				Complete(ctx, svc, epic)
			}
		}()
	})
}

// Wait blocks until pending write-backs finish (short-lived CLI processes call it before exit).
func Wait() { wg.Wait() }

func statusName(src store.JiraSource, s store.Status) string {
	var override map[string]string
	if src.StatusMap != "" {
		json.Unmarshal([]byte(src.StatusMap), &override) // malformed override: fall back to defaults
	}
	if n := override[string(s)]; n != "" {
		return n
	}
	return defaultStatusMap[s]
}

// commentBody lists the branch and PR of each repo that has a PR; "" until one exists.
func commentBody(t store.Task) string {
	var b strings.Builder
	for _, r := range t.Repos {
		if r.PRURL != nil && *r.PRURL != "" {
			fmt.Fprintf(&b, "Conveyor %s (%s)\nBranch: %s\nPR: %s\n", t.Key, r.RepoName, r.Branch, *r.PRURL)
		}
	}
	return strings.TrimSpace(b.String())
}

// writeBack pushes the task's status and PR comment; it reports the source epic and whether the task is finished.
func writeBack(ctx context.Context, svc *core.Service, id int64) (epicID int64, finished bool) {
	mu.Lock()
	defer mu.Unlock()
	t, err := store.GetTask(svc.DB, id)
	if err != nil || t.Source != "jira" || t.ExternalKey == nil {
		return 0, false
	}
	src, ok := store.JiraSourceOf(svc.DB, t)
	if !ok {
		return 0, false
	}
	key, st := *t.ExternalKey, store.GetJiraSync(svc.DB, id)
	fail := func(err error) { store.SetJiraError(svc.DB, src.EpicID, fmt.Sprintf("write-back %s: %v", key, err)) }

	if name := statusName(src, t.Status); name != "" && name != st.Status {
		if err := transition(ctx, key, name); err != nil {
			fail(err)
		} else {
			st.Status = name
		}
	}
	if body := commentBody(t); body != "" && body != st.Comment {
		if st.CommentID == "" {
			var out []byte
			if out, err = acli(ctx, "jira", "workitem", "comment", "create", "--key", key, "--body", body, "--json"); err == nil {
				var c struct{ ID json.RawMessage }
				if err = json.Unmarshal(out, &c); err == nil && len(c.ID) > 0 {
					st.CommentID, st.Comment = strings.Trim(string(c.ID), `"`), body
				} else {
					err = fmt.Errorf("parse comment id: %v", err)
				}
			}
		} else if _, err = acli(ctx, "jira", "workitem", "comment", "update", "--key", key, "--id", st.CommentID, "--body", body); err == nil {
			st.Comment = body
		}
		if err != nil {
			fail(err)
		}
	}
	if err := store.PutJiraSync(svc.DB, id, st); err != nil {
		fail(err)
	}
	return src.EpicID, t.Status == store.StatusDone || t.Status == store.StatusCancelled
}

func transition(ctx context.Context, key, status string) error {
	_, err := acli(ctx, "jira", "workitem", "transition", "--key", key, "--status", status, "--yes")
	return err
}

// Complete transitions the Jira epic to Done and marks the source done once every synced task is
// done/cancelled and Jira has no open child that Conveyor has not synced. Errors are recorded on the source.
func Complete(ctx context.Context, svc *core.Service, epicID int64) error {
	src, err := store.GetJiraSource(svc.DB, epicID)
	if err != nil || src.Done {
		return err
	}
	total, open, err := store.SyncedProgress(svc.DB, epicID)
	if err != nil || total == 0 || open > 0 {
		return err
	}
	err = func() error {
		kids, err := search(ctx, "parent = "+src.JiraKey+" AND statusCategory != Done", "key")
		if err != nil {
			return err
		}
		for _, k := range kids {
			if _, err := store.TaskByExternalKey(svc.DB, k.Key); errors.Is(err, sql.ErrNoRows) {
				return nil // unsynced open child: not complete yet
			}
		}
		if err := transition(ctx, src.JiraKey, statusName(src, store.StatusDone)); err != nil {
			return err
		}
		return store.SetJiraDone(svc.DB, epicID)
	}()
	if err != nil {
		store.SetJiraError(svc.DB, epicID, fmt.Sprintf("complete %s: %v", src.JiraKey, err))
	}
	return err
}
