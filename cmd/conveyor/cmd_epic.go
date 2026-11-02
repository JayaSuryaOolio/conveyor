package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/gh"
)

func init() {
	register("epic", runEpic, "epic builder (status|check|flows|prs|search|sessions|approve|restart|review-check)")
}

const epicUsage = "usage: conveyor epic status|check|flows|prs|search|sessions|approve|restart KEY ..."

func runEpic(home string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", epicUsage)
	}
	cmd, rest := args[0], args[1:]
	return withService(home, func(s *core.Service) error {
		fs := flag.NewFlagSet("epic "+cmd, flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "JSON output")
		flow := fs.Int("flow", 0, "flow attempt (default: the active flow)")
		fix := fs.Bool("fix", false, "repair labels")
		commits := fs.Bool("commits", false, "list each PR branch's commits")
		step := fs.Int("step", 0, "review-check: empty checks so far (backoff position)")
		reason := fs.String("reason", "", "why the flow is abandoned")
		grep := fs.String("grep", "", "text to find in transcripts")
		pos, err := parseArgs(fs, rest)
		if err != nil {
			return err
		}
		if len(pos) == 0 {
			return fmt.Errorf("%s", epicUsage)
		}
		key := pos[0]
		switch cmd {
		case "review-check":
			res, err := gh.ReviewCheck(context.Background(), s, key, *step)
			if err == nil {
				fmt.Fprintln(out, res)
			}
			return err
		case "approve":
			return s.EpicApprove(key)
		case "restart":
			return s.EpicRestart(key, *reason)
		case "status":
			v, err := s.EpicStatus(key, *flow)
			if err != nil {
				return err
			}
			if *asJSON {
				return printJSON(v)
			}
			fmt.Fprintf(out, "%s epic:%s stage %s (gate %s, %s)\n", v.Key, v.Name, v.Stage, map[bool]string{true: "on", false: "off"}[v.Gate], v.Status)
			if v.Flow != nil {
				fmt.Fprintf(out, "flow %d %s\n", v.Flow.Attempt, v.Flow.Status)
			}
			for _, t := range v.Tasks {
				fmt.Fprintf(out, "  %s %-9s %-10s %s %v\n", t.Key, t.Status, t.Kind, t.Title, t.PRs)
			}
			for _, a := range v.AC {
				fmt.Fprintf(out, "  %s done=%v covered by %v\n", a.ID, a.Done, a.Covered)
			}
			return nil
		case "check":
			f, err := s.EpicCheck(key, *fix)
			if err != nil {
				return err
			}
			for _, x := range f {
				fmt.Fprintln(out, x)
			}
			if len(f) > 0 {
				return fmt.Errorf("%d check(s) failed", len(f))
			}
			fmt.Fprintln(out, "ok")
			return nil
		case "flows":
			fl, err := s.Flows(key)
			if err != nil {
				return err
			}
			if *asJSON {
				return printJSON(fl)
			}
			for _, f := range fl {
				fmt.Fprintf(out, "flow %d  %-9s %s  %s\n", f.Attempt, f.Status, f.StartedAt, f.Reason)
			}
			return nil
		case "prs":
			v, err := s.EpicStatus(key, *flow)
			if err != nil {
				return err
			}
			var br []core.EpicBranch
			if *commits {
				if br, err = s.EpicBranches(key); err != nil {
					return err
				}
			}
			if *asJSON {
				return printJSON(map[string]any{"prs": v.PRs, "branches": br})
			}
			w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			for _, p := range v.PRs {
				fmt.Fprintf(w, "%s\t%s\t%s\n", p.TaskKey, p.RepoName, deref(p.PRURL))
			}
			w.Flush()
			for _, b := range br {
				l, err := b.Log()
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s %s/%s\n%s", b.Task, b.Repo, b.Branch, l)
			}
			return nil
		case "search":
			if len(pos) != 2 {
				return fmt.Errorf("usage: conveyor epic search KEY <regex>")
			}
			br, err := s.EpicBranches(key)
			if err != nil {
				return err
			}
			for _, b := range br {
				l, err := b.Log("-G" + pos[1])
				if err != nil {
					return err
				}
				if strings.TrimSpace(l) != "" {
					fmt.Fprintf(out, "%s %s/%s\n%s", b.Task, b.Repo, b.Branch, l)
				}
			}
			return nil
		case "sessions":
			ss, err := s.EpicSessions(key, *grep)
			if err != nil {
				return err
			}
			if *asJSON {
				return printJSON(ss)
			}
			for _, x := range ss {
				fmt.Fprintf(out, "%s run %d %s %s\n", x.Task, x.Run, x.Session, x.Transcript)
				for _, m := range x.Matches {
					fmt.Fprintf(out, "    %s\n", m)
				}
			}
			return nil
		}
		return fmt.Errorf("unknown epic command %q\n%s", cmd, epicUsage)
	})
}
