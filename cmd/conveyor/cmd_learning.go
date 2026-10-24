package main

import (
	"flag"
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/learning"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() {
	register("learning", runLearning, "curate learnings (list|accept|reject|edit|apply)")
}

func runLearning(home string, args []string) error {
	const usage = "usage: conveyor learning list [--status s] | accept ID [--target f] | reject ID | edit ID <body> | apply [--repo r]"
	if len(args) == 0 {
		return fmt.Errorf(usage)
	}
	fs := flag.NewFlagSet("learning "+args[0], flag.ContinueOnError)
	status := fs.String("status", "", "list: filter by status")
	target := fs.String("target", "", "accept: target file (default CLAUDE.md)")
	repo := fs.String("repo", "", "apply: only this repo")
	pos, err := parseArgs(fs, args[1:])
	if err != nil {
		return err
	}
	id := func(n int) (int64, error) {
		if len(pos) != n {
			return 0, fmt.Errorf(usage)
		}
		return strconv.ParseInt(pos[0], 10, 64)
	}
	return withService(home, func(s *core.Service) error {
		switch args[0] {
		case "list":
			var st []string
			if *status != "" {
				st = []string{*status}
			}
			ls, err := learning.List(s.DB, st...)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tSTATUS\tREPO\tTARGET\tBODY")
			names := map[int64]string{}
			repos, _ := store.ListRepos(s.DB)
			for _, r := range repos {
				names[r.ID] = r.Name
			}
			for _, l := range ls {
				rn := "(personal)"
				if l.RepoID != nil {
					rn = names[*l.RepoID]
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", l.ID, l.Status, rn, deref(l.Target), l.Body)
			}
			return tw.Flush()
		case "accept", "reject":
			n, err := id(1)
			if err != nil {
				return err
			}
			if args[0] == "accept" {
				err = learning.Accept(s.DB, n, *target)
			} else {
				err = learning.Reject(s.DB, n)
			}
			if err == nil {
				fmt.Fprintf(out, "%sed %d\n", args[0], n)
			}
			return err
		case "edit":
			n, err := id(2)
			if err != nil {
				return err
			}
			return learning.Edit(s.DB, n, pos[1])
		case "apply":
			keys, err := learning.Apply(s, *repo)
			if err != nil {
				return err
			}
			for _, k := range keys {
				fmt.Fprintln(out, "created", k)
			}
			if p, _ := learning.Personal(s.DB); len(p) > 0 {
				fmt.Fprintln(out, "\nPersonal learnings (paste into ~/.claude/CLAUDE.md):")
				for _, l := range p {
					fmt.Fprintf(out, "- %s\n", l.Body)
				}
			}
			return nil
		}
		return fmt.Errorf("unknown learning command %q", args[0])
	})
}
