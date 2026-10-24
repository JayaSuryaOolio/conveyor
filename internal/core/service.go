package core

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/claude"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// Service bundles the DB and home dir for domain operations.
type Service struct {
	DB   *sql.DB
	Home string
	// ProjectsDir overrides ~/.claude/projects (tests).
	ProjectsDir string
}

// NewTaskInput describes a task to create. Repos are registered repo names;
// Primary defaults to the first.
type NewTaskInput struct {
	Title       string
	Description string
	Repos       []string
	Primary     string
	Start       bool // true: status todo instead of backlog
	Model       string
	Agent       string
	ParentKey   string
	Priority    int
	// Source, ExternalKey and ExternalURL come from importers (default source is manual).
	Source      string
	ExternalKey string
	ExternalURL string
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// slug lowercases s, collapses non-alphanumerics to "-" and truncates to max.
func slug(s string, max int) string {
	s = strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > max {
		s = strings.Trim(s[:max], "-")
	}
	if s == "" {
		s = "task"
	}
	return s
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// CreateTask validates the repos and creates the task.
func (s *Service) CreateTask(in NewTaskInput) (store.Task, error) {
	if strings.TrimSpace(in.Title) == "" {
		return store.Task{}, fmt.Errorf("title is required")
	}
	if len(in.Repos) == 0 {
		return store.Task{}, fmt.Errorf("at least one repo is required")
	}
	primary := in.Primary
	if primary == "" {
		primary = in.Repos[0]
	}
	sl := slug(in.Title, 40)
	var links []store.TaskRepo
	seen := map[string]bool{}
	foundPrimary := false
	for _, name := range in.Repos {
		if seen[name] {
			continue
		}
		seen[name] = true
		r, err := store.GetRepoByName(s.DB, name)
		if err != nil {
			return store.Task{}, fmt.Errorf("repo %q is not registered: %w", name, err)
		}
		isPrimary := name == primary
		foundPrimary = foundPrimary || isPrimary
		links = append(links, store.TaskRepo{RepoID: r.ID, IsPrimary: isPrimary, Branch: sl})
	}
	if !foundPrimary {
		return store.Task{}, fmt.Errorf("primary repo %q is not among the task's repos", primary)
	}
	t := store.Task{
		Title: in.Title, Description: in.Description, Priority: in.Priority,
		Model: optional(in.Model), Agent: optional(in.Agent), Status: store.StatusBacklog,
		Source: in.Source, ExternalKey: optional(in.ExternalKey), ExternalURL: optional(in.ExternalURL),
	}
	if in.Start {
		t.Status = store.StatusTodo
	}
	if in.ParentKey != "" {
		p, err := store.GetTaskByKey(s.DB, in.ParentKey)
		if err != nil {
			return store.Task{}, fmt.Errorf("parent: %w", err)
		}
		t.ParentID = &p.ID
	}
	return store.CreateTask(s.DB, t, links)
}

// AddComment appends a comment. A human or system comment on a task in
// needs_you, review, failed or done requeues it as todo.
func (s *Service) AddComment(key, author, body string) (store.Comment, error) {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return store.Comment{}, err
	}
	c, err := store.AddComment(s.DB, store.Comment{TaskID: t.ID, Author: author, Body: body})
	if err != nil {
		return store.Comment{}, err
	}
	var actor store.Actor
	switch author {
	case store.AuthorHuman:
		actor = store.ActorHuman
	case store.AuthorSystem:
		actor = store.ActorSystem
	default:
		return c, nil
	}
	switch t.Status {
	case store.StatusNeedsYou, store.StatusReview, store.StatusFailed, store.StatusDone:
		if _, err := store.UpdateTaskStatus(s.DB, t.ID, store.StatusTodo, actor); err != nil {
			return c, fmt.Errorf("requeue after comment: %w", err)
		}
	}
	return c, nil
}

// Move changes a task's status as a human. Moving to done or cancelled also stops
// and removes the task's background sessions (best effort).
func (s *Service) Move(key string, to store.Status) error {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return err
	}
	if _, err = store.UpdateTaskStatus(s.DB, t.ID, to, store.ActorHuman); err != nil {
		return err
	}
	if to == store.StatusDone || to == store.StatusCancelled {
		s.cleanup(t.ID)
	}
	return nil
}

// bgIDs returns the bg ids of the task's runs, each once.
func (s *Service) bgIDs(taskID int64) []string {
	runs, _ := store.ListRuns(s.DB, taskID)
	seen := map[string]bool{}
	var ids []string
	for _, r := range runs {
		if r.BgID != nil && !seen[*r.BgID] {
			seen[*r.BgID] = true
			ids = append(ids, *r.BgID)
		}
	}
	return ids
}

func (s *Service) cleanup(taskID int64) {
	for _, id := range s.bgIDs(taskID) {
		claude.Stop(context.Background(), id)
		claude.Remove(context.Background(), id)
	}
}

// Stop stops the task's background Claude sessions.
func (s *Service) Stop(key string) error {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return err
	}
	runs, err := store.ActiveRuns(s.DB)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if r.TaskID == t.ID && r.BgID != nil {
			if err := claude.Stop(context.Background(), *r.BgID); err != nil {
				return err
			}
		}
	}
	return nil
}

// MarkDone moves a task to done.
func (s *Service) MarkDone(key string) error { return s.Move(key, store.StatusDone) }

// Cancel moves a task to cancelled and stops its sessions.
func (s *Service) Cancel(key string) error { return s.Move(key, store.StatusCancelled) }
