package main

import (
	"fmt"
	"text/tabwriter"

	"flag"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() { register("repo", runRepo, "manage registered repos (add|list|rm)") }

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
			r, err := s.AddRepo(pos[0], *name)
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
