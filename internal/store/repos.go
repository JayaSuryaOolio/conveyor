package store

import (
	"database/sql"
	"fmt"
)

const repoCols = `id, name, path, default_branch, created_at, setup_script`

func scanRepo(r interface{ Scan(...any) error }) (Repo, error) {
	var x Repo
	err := r.Scan(&x.ID, &x.Name, &x.Path, &x.DefaultBranch, &x.CreatedAt, &x.SetupScript)
	return x, err
}

// AddRepo registers a repo; name and path must be unique.
func AddRepo(db *sql.DB, r Repo) (Repo, error) {
	r.CreatedAt = now()
	res, err := db.Exec(`INSERT INTO repos (name, path, default_branch, created_at) VALUES (?,?,?,?)`,
		r.Name, r.Path, r.DefaultBranch, r.CreatedAt)
	if err != nil {
		return Repo{}, fmt.Errorf("add repo %q: %w", r.Name, err)
	}
	r.ID, _ = res.LastInsertId()
	return r, nil
}

// ListRepos returns all repos ordered by name.
func ListRepos(db *sql.DB) ([]Repo, error) {
	rows, err := db.Query(`SELECT ` + repoCols + ` FROM repos ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer rows.Close()
	var out []Repo
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRepoByName returns the repo or a wrapped sql.ErrNoRows.
func GetRepoByName(db *sql.DB, name string) (Repo, error) {
	r, err := scanRepo(db.QueryRow(`SELECT `+repoCols+` FROM repos WHERE name=?`, name))
	if err != nil {
		return Repo{}, fmt.Errorf("repo %q: %w", name, err)
	}
	return r, nil
}

// SetRepoSetupScript sets the script run in each new worktree of the repo; "" clears it.
func SetRepoSetupScript(db *sql.DB, name, script string) error {
	res, err := db.Exec(`UPDATE repos SET setup_script=? WHERE name=?`, script, name)
	if err != nil {
		return fmt.Errorf("set setup script %q: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("repo %q: %w", name, sql.ErrNoRows)
	}
	return nil
}

// RemoveRepo deletes a repo; it fails while tasks reference it.
func RemoveRepo(db *sql.DB, name string) error {
	res, err := db.Exec(`DELETE FROM repos WHERE name=?`, name)
	if err != nil {
		return fmt.Errorf("remove repo %q: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("repo %q: %w", name, sql.ErrNoRows)
	}
	return nil
}

// AddTaskRepo links a repo to a task as a non-primary repo on the given branch.
func AddTaskRepo(db *sql.DB, taskID, repoID int64, branch string) error {
	if _, err := db.Exec(`INSERT INTO task_repos (task_id, repo_id, is_primary, branch) VALUES (?,?,0,?)`, taskID, repoID, branch); err != nil {
		return fmt.Errorf("add task repo: %w", err)
	}
	return nil
}

// RemoveTaskRepo unlinks a repo from a task.
func RemoveTaskRepo(db *sql.DB, taskID, repoID int64) error {
	if _, err := db.Exec(`DELETE FROM task_repos WHERE task_id=? AND repo_id=?`, taskID, repoID); err != nil {
		return fmt.Errorf("remove task repo: %w", err)
	}
	return nil
}
