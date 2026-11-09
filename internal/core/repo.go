package core

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
)

// AddRepo validates that path is the top level of a git clone, detects its
// default branch (origin/HEAD, else the current branch) and registers it.
// name defaults to the basename of path.
func (s *Service) AddRepo(path, name string) (store.Repo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return store.Repo{}, fmt.Errorf("resolve path: %w", err)
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return store.Repo{}, fmt.Errorf("resolve path: %w", err)
	}
	top, err := workspace.Git(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return store.Repo{}, fmt.Errorf("%s is not a git repository: %w", abs, err)
	}
	if top, err = filepath.EvalSymlinks(top); err != nil || top != abs {
		return store.Repo{}, fmt.Errorf("%s is not the top level of a git repo (top level is %s)", abs, top)
	}
	branch := ""
	if ref, err := workspace.Git(abs, "symbolic-ref", "refs/remotes/origin/HEAD"); err == nil {
		branch = strings.TrimPrefix(ref, "refs/remotes/origin/")
	}
	if branch == "" {
		branch, _ = workspace.Git(abs, "branch", "--show-current")
	}
	if branch == "" {
		return store.Repo{}, fmt.Errorf("cannot detect default branch of %s", abs)
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	return store.AddRepo(s.DB, store.Repo{Name: name, Path: abs, DefaultBranch: branch})
}
