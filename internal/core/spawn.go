package core

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// SpawnChildren creates a backlog child task per "title | description" line, with the parent's repos (same primary)
// and parent_id set, plus the parent's epic:<name> label when it is an epic member. A "[todo]" prefix on the title creates it
// in todo instead, so the dispatcher starts it. A line without a description is skipped and reported in the error: the child's agent needs one.
func SpawnChildren(db *sql.DB, parent store.Task, lines []string) ([]store.Task, error) {
	var out []store.Task
	var errs []error
	for _, line := range lines {
		title, desc, _ := strings.Cut(line, "|")
		title, desc = strings.TrimSpace(title), strings.TrimSpace(desc)
		status := store.StatusBacklog
		if rest, ok := strings.CutPrefix(title, "[todo]"); ok {
			title, status = strings.TrimSpace(rest), store.StatusTodo
		}
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
		t, err := store.CreateTask(db, store.Task{Title: title, Description: desc, Status: status, ParentID: &parent.ID}, links)
		if err != nil {
			errs = append(errs, fmt.Errorf("spawn %q: %w", title, err))
			continue
		}
		if name := store.EpicName(parent); name != "" {
			if err := store.SetPrefixedLabel(db, t.ID, store.PrefixEpic+name); err != nil {
				errs = append(errs, fmt.Errorf("spawn %q: epic label: %w", title, err))
			}
		}
		out = append(out, t)
	}
	return out, errors.Join(errs...)
}
