// Package jira imports Jira issues as backlog tasks through the acli CLI.
package jira

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

type issue struct {
	Key    string `json:"key"`
	Self   string `json:"self"`
	Fields struct {
		Summary     string          `json:"summary"`
		Labels      []string        `json:"labels"`
		Description json.RawMessage `json:"description"`
	} `json:"fields"`
}

// adfText flattens an Atlassian Document Format node (or a plain string) to text.
func adfText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n struct {
		Type    string            `json:"type"`
		Text    string            `json:"text"`
		Content []json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &n) != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(n.Text)
	for _, c := range n.Content {
		b.WriteString(adfText(c))
	}
	switch n.Type {
	case "paragraph", "heading", "listItem", "codeBlock", "blockquote":
		b.WriteString("\n")
	case "hardBreak":
		b.WriteString("\n")
	}
	return b.String()
}

// search runs `acli jira workitem search` for jql and returns the issues with the given fields.
func search(ctx context.Context, jql, fields string) ([]issue, error) {
	out, err := exec.CommandContext(ctx, "acli", "jira", "workitem", "search", "--jql", jql,
		"--fields", fields, "--paginate", "--json").Output()
	if err != nil {
		var ee *exec.ExitError
		switch {
		case errors.Is(err, exec.ErrNotFound):
			return nil, errors.New("acli not found on PATH: install it and run `acli jira auth login`")
		case errors.As(err, &ee):
			return nil, fmt.Errorf("acli failed (not authenticated? run `acli jira auth login`): %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("acli: %w", err)
	}
	var issues []issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("parse acli output: %w", err)
	}
	return issues, nil
}

// Import runs `acli jira workitem search` for jql and creates a backlog task in
// repo for each issue whose key is not yet an external_key. It returns the new task keys.
// The task URL is <jira_base_url>/browse/KEY when that setting is set, else the issue's API URL.
func Import(ctx context.Context, svc *core.Service, jql, repo string) ([]string, error) {
	issues, err := search(ctx, jql, "key,summary,description")
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(store.GetSetting(svc.DB, "jira_base_url"), "/")
	var created []string
	for _, is := range issues {
		var exists int
		err := svc.DB.QueryRow(`SELECT 1 FROM tasks WHERE external_key=?`, is.Key).Scan(&exists)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return created, fmt.Errorf("check %s: %w", is.Key, err)
		}
		url := is.Self
		if base != "" {
			url = base + "/browse/" + is.Key
		}
		t, err := svc.CreateTask(core.NewTaskInput{
			Title: is.Fields.Summary, Description: strings.TrimSpace(adfText(is.Fields.Description)), Repos: []string{repo},
			Source: "jira", CreatedBy: "jira", ExternalKey: is.Key, ExternalURL: url,
		})
		if err != nil {
			return created, fmt.Errorf("import %s: %w", is.Key, err)
		}
		created = append(created, t.Key)
	}
	return created, nil
}
