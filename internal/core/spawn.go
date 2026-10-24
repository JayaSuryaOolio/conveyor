package core

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// SpawnChildren creates a backlog child task per "title | description" line, with the parent's repos (same primary)
// and parent_id set. A line without a description is skipped and reported in the error: the child's agent needs one.
func SpawnChildren(db *sql.DB, parent store.Task, lines []string) ([]store.Task, error) {
	var out []store.Task
	var errs []error
	for _, line := range lines {
		title, desc, _ := strings.Cut(line, "|")
		title, desc = strings.TrimSpace(title), strings.TrimSpace(desc)
		if title == "" {
			continue
		}
		if desc == "" {
			errs = append(errs, fmt.Errorf("spawn %q: no description (use `spawn: title | description`)", title))
			continue
		}
		links := make([]store.TaskRepo, len(parent.Repos))
		for i, r := range parent.Repos {
			links[i] = store.TaskRepo{RepoID: r.RepoID, IsPrimary: r.IsPrimary, Branch: slug(title, 40)}
		}
		t, err := store.CreateTask(db, store.Task{Title: title, Description: desc, ParentID: &parent.ID}, links)
		if err != nil {
			errs = append(errs, fmt.Errorf("spawn %q: %w", title, err))
			continue
		}
		out = append(out, t)
	}
	return out, errors.Join(errs...)
}
