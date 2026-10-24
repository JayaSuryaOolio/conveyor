package main

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"flag"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// execCommand is swappable in tests.
var execCommand = exec.CommandContext

func init() { register("repo", runRepo, "manage registered repos (add|list|rm)") }

func git(path string, args ...string) (string, error) {
	cmd := execCommand(context.Background(), "git", append([]string{"-C", path}, args...)...)
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err
}

func runRepo(home string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: conveyor repo add <path> [--name n] | list | rm <name>")
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("repo add", flag.ContinueOnError)
		name := fs.String("name", "", "short name (default basename of path)")
		pos, err := parseArgs(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return fmt.Errorf("usage: conveyor repo add <path> [--name n]")
		}
		return withService(home, func(s *core.Service) error {
			r, err := addRepo(s, pos[0], *name)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "added %s (%s, default branch %s)\n", r.Name, r.Path, r.DefaultBranch)
			return nil
		})
	case "list":
		fs := flag.NewFlagSet("repo list", flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "JSON output")
		if _, err := parseArgs(fs, args[1:]); err != nil {
			return err
		}
		return withService(home, func(s *core.Service) error {
			repos, err := store.ListRepos(s.DB)
			if err != nil {
				return err
			}
			if *asJSON {
				if repos == nil {
					repos = []store.Repo{}
				}
				return printJSON(repos)
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tBRANCH\tPATH")
			for _, r := range repos {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Name, r.DefaultBranch, r.Path)
			}
			return tw.Flush()
		})
	case "rm":
		if len(args) != 2 {
			return fmt.Errorf("usage: conveyor repo rm <name>")
		}
		return withService(home, func(s *core.Service) error {
			if err := store.RemoveRepo(s.DB, args[1]); err != nil {
				return err
			}
			fmt.Fprintf(out, "removed %s\n", args[1])
			return nil
		})
	}
	return fmt.Errorf("unknown repo command %q", args[0])
}

func addRepo(s *core.Service, path, name string) (store.Repo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return store.Repo{}, fmt.Errorf("resolve path: %w", err)
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return store.Repo{}, fmt.Errorf("resolve path: %w", err)
	}
	top, err := git(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return store.Repo{}, fmt.Errorf("%s is not a git repository: %w", abs, err)
	}
	if top, err = filepath.EvalSymlinks(top); err != nil || top != abs {
		return store.Repo{}, fmt.Errorf("%s is not the top level of a git repo (top level is %s)", abs, top)
	}
	branch := ""
	if ref, err := git(abs, "symbolic-ref", "refs/remotes/origin/HEAD"); err == nil {
		branch = strings.TrimPrefix(ref, "refs/remotes/origin/")
	}
	if branch == "" {
		branch, _ = git(abs, "branch", "--show-current")
	}
	if branch == "" {
		return store.Repo{}, fmt.Errorf("cannot detect default branch of %s", abs)
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	return store.AddRepo(s.DB, store.Repo{Name: name, Path: abs, DefaultBranch: branch})
}
