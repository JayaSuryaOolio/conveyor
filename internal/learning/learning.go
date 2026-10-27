// Package learning curates proposed learnings and turns accepted ones into "apply learnings" tasks.
package learning

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
)

// DefaultTarget is the file an accepted repo-file learning goes to when Apply runs.
const DefaultTarget = "CLAUDE.md"

// Local is the target that keeps a repo's learning in Conveyor's data folder (workspace.LearningsFile) instead of
// the repo: committing to the repo is a team decision, so it is the default for repo learnings.
const Local = "local"

// List returns learnings, optionally filtered by status.
func List(db *sql.DB, status ...string) ([]store.Learning, error) {
	return store.ListLearnings(db, status...)
}

// Accept accepts a learning for target. A repo learning with target "" or Local is appended to the repo's
// LEARNINGS.md under home and marked applied; any other target (or a personal learning, default CLAUDE.md) waits
// for Apply.
func Accept(db *sql.DB, home string, id int64, target string) error {
	target = strings.TrimSpace(target)
	l, err := store.GetLearning(db, id)
	if err != nil {
		return err
	}
	if l.RepoID != nil && (target == "" || target == Local) {
		var repo string
		if err := db.QueryRow(`SELECT name FROM repos WHERE id=?`, *l.RepoID).Scan(&repo); err != nil {
			return fmt.Errorf("learning %d repo: %w", id, err)
		}
		target = Local
		l.Status, l.Target = store.LearningApplied, &target
		if err := store.UpdateLearning(db, l); err != nil { // first, so an applied learning is never appended twice
			return err
		}
		return appendLine(workspace.LearningsFile(home, repo), "- "+l.Body+"\n")
	}
	if target == "" || target == Local {
		target = DefaultTarget
	}
	l.Status, l.Target = store.LearningAccepted, &target
	return store.UpdateLearning(db, l)
}

func appendLine(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Reject marks a learning rejected.
func Reject(db *sql.DB, id int64) error {
	l, err := store.GetLearning(db, id)
	if err != nil {
		return err
	}
	l.Status = store.LearningRejected
	return store.UpdateLearning(db, l)
}

// Edit replaces a learning's body.
func Edit(db *sql.DB, id int64, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("learning body is empty")
	}
	l, err := store.GetLearning(db, id)
	if err != nil {
		return err
	}
	l.Body = strings.TrimSpace(body)
	return store.UpdateLearning(db, l)
}

// Personal returns accepted learnings with no repo, for the human to paste into ~/.claude/CLAUDE.md.
func Personal(db *sql.DB) ([]store.Learning, error) {
	all, err := store.ListLearnings(db, store.LearningAccepted)
	var out []store.Learning
	for _, l := range all {
		if l.RepoID == nil {
			out = append(out, l)
		}
	}
	return out, err
}

const instruction = "Edit the target files so each lesson is captured concisely and isn't duplicated; open one PR."

// Apply creates one todo task "Apply learnings to <repo>" per repo that has accepted learnings (repo "" means every
// repo, otherwise just that one) and marks those learnings applied. Personal learnings are skipped (see Personal).
// It returns the created task keys.
func Apply(svc *core.Service, repo string) ([]string, error) {
	repos, err := store.ListRepos(svc.DB)
	if err != nil {
		return nil, err
	}
	if repo != "" {
		if _, err := store.GetRepoByName(svc.DB, repo); err != nil {
			return nil, err
		}
	}
	accepted, err := store.ListLearnings(svc.DB, store.LearningAccepted)
	if err != nil {
		return nil, err
	}
	byRepo := map[int64][]store.Learning{}
	for _, l := range accepted {
		if l.RepoID != nil {
			byRepo[*l.RepoID] = append(byRepo[*l.RepoID], l)
		}
	}
	var keys []string
	for _, r := range repos {
		ls := byRepo[r.ID]
		if len(ls) == 0 || (repo != "" && r.Name != repo) {
			continue
		}
		var sb strings.Builder
		sb.WriteString("Accepted learnings for " + r.Name + ":\n\n")
		for _, l := range ls {
			src := "unknown task"
			if l.TaskID != nil {
				if t, err := store.GetTask(svc.DB, *l.TaskID); err == nil {
					src = t.Key
				}
			}
			target := DefaultTarget
			if l.Target != nil {
				target = *l.Target
			}
			fmt.Fprintf(&sb, "- %s (target: %s, from %s)\n", l.Body, target, src)
		}
		sb.WriteString("\n" + instruction + "\n")
		title := "Apply learnings to " + r.Name
		t, found, err := findPending(svc.DB, title, sb.String())
		if err != nil {
			return keys, err
		}
		if !found {
			if t, err = svc.CreateTask(core.NewTaskInput{
				Title: title, Description: sb.String(), Repos: []string{r.Name}, Start: true, Source: "learning", CreatedBy: "learning",
			}); err != nil {
				return keys, err
			}
		}
		for _, l := range ls {
			l.Status, l.AppliedTaskID = store.LearningApplied, &t.ID
			if err := store.UpdateLearning(svc.DB, l); err != nil {
				return keys, err
			}
		}
		keys = append(keys, t.Key)
	}
	return keys, nil
}

// findPending returns a live learning task with exactly this title and description: one a previous Apply created
// before failing to mark its learnings applied. Reusing it keeps a retry from creating a duplicate.
func findPending(db *sql.DB, title, desc string) (store.Task, bool, error) {
	tasks, err := store.ListTasks(db)
	if err != nil {
		return store.Task{}, false, err
	}
	for _, t := range tasks {
		if t.Source == "learning" && t.Title == title && t.Description == desc &&
			t.Status != store.StatusDone && t.Status != store.StatusCancelled {
			return t, true, nil
		}
	}
	return store.Task{}, false, nil
}
