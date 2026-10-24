package core

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// SpawnChildren creates a backlog child task per title, with the parent's repos (same primary) and parent_id set.
func SpawnChildren(db *sql.DB, parent store.Task, titles []string) ([]store.Task, error) {
	var out []store.Task
	for _, title := range titles {
		if title = strings.TrimSpace(title); title == "" {
			continue
		}
		links := make([]store.TaskRepo, len(parent.Repos))
		for i, r := range parent.Repos {
			links[i] = store.TaskRepo{RepoID: r.RepoID, IsPrimary: r.IsPrimary, Branch: slug(title, 40)}
		}
		t, err := store.CreateTask(db, store.Task{Title: title, ParentID: &parent.ID}, links)
		if err != nil {
			return out, fmt.Errorf("spawn %q: %w", title, err)
		}
		out = append(out, t)
	}
	return out, nil
}
