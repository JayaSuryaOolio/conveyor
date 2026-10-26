package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() {
	register("agent", runAgent, "Claude subagents from repos and ~/.claude (list|add|model)")
}

const agentUsage = "usage: conveyor agent list [--repo r] | add <name> --desc d (--prompt p | --prompt-file f) [--model m] [--repo r] | model <name> <model|\"\"> [--repo r] (\"\" inherits default_model; see conveyor models)"

func runAgent(home string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", agentUsage)
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
		repo := fs.String("repo", "", "repo name(s), comma separated (default: all registered)")
		asJSON := fs.Bool("json", false, "JSON output")
		if _, err := parseArgs(fs, args[1:]); err != nil {
			return err
		}
		return withService(home, func(s *core.Service) error {
			names := splitCSV(*repo)
			if *repo == "" {
				rs, err := store.ListRepos(s.DB)
				if err != nil {
					return err
				}
				for _, r := range rs {
					names = append(names, r.Name)
				}
			}
			as, err := s.ListAgents(names)
			if err != nil {
				return err
			}
			ml := s.Models()
			def := store.GetSetting(s.DB, "default_model")
			for i, a := range as {
				if a.Model != "" && a.Model != "inherit" {
					as[i].Resolved = ml.Resolve(a.Model)
				}
			}
			if *asJSON {
				return printJSON(as)
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tSCOPE\tMODEL\tDESCRIPTION")
			for _, a := range as {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Name, a.Scope, describeModel(a, def), a.Description)
			}
			return tw.Flush()
		})
	case "add":
		fs := flag.NewFlagSet("agent add", flag.ContinueOnError)
		repo := fs.String("repo", "", "repo name (default: user scope, ~/.claude/agents)")
		desc := fs.String("desc", "", "one-line description")
		model := fs.String("model", "", "default model: a tier alias or id from `conveyor models`, or inherit")
		prompt := fs.String("prompt", "", "system prompt")
		promptFile := fs.String("prompt-file", "", "read the prompt from a file")
		pos, err := parseArgs(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return fmt.Errorf("%s", agentUsage)
		}
		p := *prompt
		if *promptFile != "" {
			b, err := os.ReadFile(*promptFile)
			if err != nil {
				return fmt.Errorf("read prompt file: %w", err)
			}
			p = string(b)
		}
		scope := *repo
		if scope == "" {
			scope = "user"
		}
		return withService(home, func(s *core.Service) error {
			if err := s.AddAgent(pos[0], *desc, p, scope, *model); err != nil {
				return err
			}
			fmt.Fprintf(out, "added agent %s (%s)\n", pos[0], scope)
			if scope != "user" {
				fmt.Fprintln(out, "commit it in the repo so task worktrees see it")
			}
			return nil
		})
	case "model":
		fs := flag.NewFlagSet("agent model", flag.ContinueOnError)
		repo := fs.String("repo", "", "repo name (default: user scope)")
		pos, err := parseArgs(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 2 {
			return fmt.Errorf("%s", agentUsage)
		}
		scope := *repo
		if scope == "" {
			scope = "user"
		}
		return withService(home, func(s *core.Service) error {
			if err := s.SetAgentModel(scope, pos[0], pos[1]); err != nil {
				return err
			}
			fmt.Fprintf(out, "agent %s (%s) model: %q\n", pos[0], scope, pos[1])
			return nil
		})
	}
	return fmt.Errorf("unknown agent command %q\n%s", args[0], agentUsage)
}

// describeModel shows what an agent's model runs as: the id an alias resolves
// to and the inherited default for a blank.
func describeModel(a core.Agent, def string) string {
	switch {
	case a.Model == "":
		if def == "" {
			def = "Claude Code default"
		}
		return "(default: " + def + ")"
	case a.Resolved != "" && a.Resolved != a.Model:
		return a.Model + " -> " + a.Resolved
	}
	return a.Model
}
