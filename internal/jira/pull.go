package jira

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

const (
	pullFields    = "key,summary,description,labels"
	repoLabel     = "repo:"
	LabelNeedRepo = "needs-repo" // task has neither a repo:<name> Jira label nor a default repo
)

// Pull syncs the epic's Jira source: one backlog task per child issue, sub-tasks parented to
// their child, idempotent on external_key. It returns the new task keys. The outcome lands in
// the source's last_error; last_sync advances only on success.
func Pull(ctx context.Context, svc *core.Service, epicKey string) ([]string, error) {
	epic, err := store.GetTaskByKey(svc.DB, epicKey)
	if err != nil {
		return nil, err
	}
	src, err := store.GetJiraSource(svc.DB, epic.ID)
	if err != nil {
		return nil, err
	}
	start := time.Now().UTC()
	created, err := pull(ctx, svc, epic, src)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if rerr := store.RecordJiraSync(svc.DB, epic.ID, start.Format(time.RFC3339), msg); rerr != nil && err == nil {
		err = rerr
	}
	if err == nil {
		Complete(ctx, svc, epic.ID) // failures land in last_error
	}
	return created, err
}

func pull(ctx context.Context, svc *core.Service, epic store.Task, src store.JiraSource) ([]string, error) {
	// Relative JQL (-Nm) avoids Jira's timezone-dependent absolute dates; the overlap is harmless (idempotent).
	since := ""
	if src.LastSync != nil {
		if lt, err := time.Parse(time.RFC3339, *src.LastSync); err == nil {
			since = fmt.Sprintf(" AND updated >= -%dm", int(time.Since(lt).Minutes())+2)
		}
	}
	var created []string
	add := func(is issue, parentKey string) error {
		k, err := upsert(svc, epic, src, is, parentKey)
		if k != "" {
			created = append(created, k)
		}
		return err
	}
	children, err := search(ctx, "parent = "+src.JiraKey+since, pullFields)
	if err != nil {
		return created, err
	}
	for _, is := range children {
		if err := add(is, epic.Key); err != nil {
			return created, err
		}
	}
	// acli cannot return an issue's parent, so ask per synced child to know which task owns each sub-task.
	// ponytail: one acli call per child; batch `parent in (...)` if epics grow past ~100 children.
	rows, err := svc.DB.Query(`SELECT key, external_key FROM tasks WHERE parent_id=? AND external_key IS NOT NULL`, epic.ID)
	if err != nil {
		return created, fmt.Errorf("list synced children: %w", err)
	}
	type pair struct{ task, jira string }
	var kids []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.task, &p.jira); err != nil {
			rows.Close()
			return created, err
		}
		kids = append(kids, p)
	}
	rows.Close() // single connection: free it before creating tasks
	for _, k := range kids {
		subs, err := search(ctx, "parent = "+k.jira+since, pullFields)
		if err != nil {
			return created, err
		}
		for _, is := range subs {
			if err := add(is, k.task); err != nil {
				return created, err
			}
		}
	}
	return created, nil
}

// upsert creates the task for an unseen issue (returning its key) or refreshes title and
// description of one that has not started.
func upsert(svc *core.Service, epic store.Task, src store.JiraSource, is issue, parentKey string) (string, error) {
	desc := strings.TrimSpace(adfText(is.Fields.Description))
	t, err := store.TaskByExternalKey(svc.DB, is.Key)
	if err == nil {
		if (t.Status == store.StatusBacklog || t.Status == store.StatusTodo) && (t.Title != is.Fields.Summary || t.Description != desc) {
			t.Title, t.Description = is.Fields.Summary, desc
			if err := store.UpdateTask(svc.DB, t); err != nil {
				return "", err
			}
		}
		return "", nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("check %s: %w", is.Key, err)
	}

	repo := ""
	for _, l := range is.Fields.Labels {
		if n, ok := strings.CutPrefix(l, repoLabel); ok && repoKnown(svc, n) {
			repo = n
			break
		}
	}
	if repo == "" && repoKnown(svc, src.DefaultRepo) {
		repo = src.DefaultRepo
	}
	url := is.Self
	if base := strings.TrimRight(store.GetSetting(svc.DB, "jira_base_url"), "/"); base != "" {
		url = base + "/browse/" + is.Key
	}
	in := core.NewTaskInput{
		Title: is.Fields.Summary, Description: desc, ParentKey: parentKey,
		Source: "jira", CreatedBy: "jira", ExternalKey: is.Key, ExternalURL: url,
	}
	if repo != "" {
		in.Repos = []string{repo} // else the task inherits its parent's repos
	}
	t, err = svc.CreateTask(in)
	if err != nil {
		return "", fmt.Errorf("import %s: %w", is.Key, err)
	}
	labels := []string{store.PrefixEpic + store.EpicName(epic)}
	for _, r := range t.Repos {
		labels = append(labels, repoLabel+r.RepoName)
	}
	if repo == "" && parentKey == epic.Key {
		labels = append(labels, LabelNeedRepo)
	}
	for _, l := range labels {
		if err := store.AddLabel(svc.DB, t.ID, l); err != nil {
			return t.Key, err
		}
	}
	return t.Key, nil
}

func repoKnown(svc *core.Service, name string) bool {
	if name == "" {
		return false
	}
	_, err := store.GetRepoByName(svc.DB, name)
	return err == nil
}
