package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/jira"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() {
	register("jira", runJira, "Jira epic source (source EPIC [--key K --repo R --interval S --status-map JSON | --clear] | sync EPIC)")
}

const jiraUsage = "usage: conveyor jira source EPIC [--key PROJ-1 --repo R --interval SEC --status-map JSON | --clear] | sync EPIC"

func runJira(home string, args []string) error {
	if len(args) < 2 || (args[0] != "source" && args[0] != "sync") {
		return errors.New(jiraUsage)
	}
	cmd := args[0]
	fs := flag.NewFlagSet("jira "+cmd, flag.ContinueOnError)
	key := fs.String("key", "", "Jira epic key")
	repo := fs.String("repo", "", "default repo for tasks without a repo:<name> label")
	interval := fs.Int("interval", 0, "poll interval in seconds (default 300)")
	smap := fs.String("status-map", "", "JSON status -> Jira status overrides")
	clear := fs.Bool("clear", false, "detach the source")
	pos, err := parseArgs(fs, args[1:])
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New(jiraUsage)
	}
	epic := pos[0]
	return withService(home, func(s *core.Service) error {
		if cmd == "sync" {
			keys, err := jira.Pull(context.Background(), s, epic)
			for _, k := range keys {
				fmt.Fprintln(out, "created", k)
			}
			return err
		}
		switch {
		case *clear:
			return jira.ClearSource(s, epic)
		case *key != "":
			return jira.SetSource(s, epic, store.JiraSource{JiraKey: *key, DefaultRepo: *repo, IntervalSec: *interval, StatusMap: *smap})
		}
		src, err := jira.GetSource(s, epic)
		if err != nil {
			return err
		}
		if src == nil {
			fmt.Fprintln(out, "no jira source")
			return nil
		}
		fmt.Fprintf(out, "key=%s repo=%s interval=%ds last_sync=%s done=%v\nerror=%s\n", src.JiraKey, src.DefaultRepo, src.IntervalSec, deref(src.LastSync), src.Done, src.LastError)
		return nil
	})
}
