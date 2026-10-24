package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/automation"
	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/jira"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() {
	register("automation", runAutomation, "manage automations (list|add|enable|disable|run|rm)")
	register("import", runImport, "import tasks (jira --jql ... --repo a)")
}

const automationUsage = "usage: conveyor automation list | add NAME --schedule S --kind K --arg JSON [--precheck CMD] | add --template pr-sweep | enable|disable|run|rm NAME"

func runAutomation(home string, args []string) error {
	if len(args) == 0 {
		return errors.New(automationUsage)
	}
	cmd, rest := args[0], args[1:]
	return withService(home, func(s *core.Service) error {
		switch cmd {
		case "list":
			as, err := store.ListAutomations(s.DB)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tENABLED\tSCHEDULE\tKIND\tLAST RUN\tLAST RESULT")
			for _, a := range as {
				fmt.Fprintf(tw, "%s\t%v\t%s\t%s\t%s\t%s\n", a.Name, a.Enabled, a.Schedule, a.ActionKind, deref(a.LastRunAt), deref(a.LastResult))
			}
			return tw.Flush()
		case "add":
			return automationAdd(s, rest)
		case "enable", "disable", "rm", "run":
			if len(rest) != 1 {
				return fmt.Errorf("usage: conveyor automation %s NAME", cmd)
			}
			if cmd == "rm" {
				return store.DeleteAutomation(s.DB, rest[0])
			}
			a, err := store.GetAutomation(s.DB, rest[0])
			if err != nil {
				return err
			}
			if cmd == "run" {
				res, err := automation.RunOne(context.Background(), s, a)
				if err != nil {
					res = "error: " + err.Error()
				}
				if rerr := automation.Record(s, a, res, time.Now()); rerr != nil {
					return rerr
				}
				fmt.Fprintln(out, res)
				return err
			}
			a.Enabled = cmd == "enable"
			return store.UpdateAutomation(s.DB, a)
		}
		return fmt.Errorf("unknown automation command %q\n%s", cmd, automationUsage)
	})
}

func automationAdd(s *core.Service, args []string) error {
	fs := flag.NewFlagSet("automation add", flag.ContinueOnError)
	tmpl := fs.String("template", "", "built-in template (pr-sweep)")
	schedule := fs.String("schedule", "", "cron schedule (5 fields)")
	precheck := fs.String("precheck", "", "shell precheck; exit 0 runs the action")
	kind := fs.String("kind", "", "create_task, comment or cli")
	arg := fs.String("arg", "", "action_arg JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	var a store.Automation
	if *tmpl != "" {
		t, ok := automation.Templates[*tmpl]
		if !ok {
			return fmt.Errorf("unknown template %q", *tmpl)
		}
		a = t
	} else {
		a = store.Automation{Schedule: *schedule, Precheck: *precheck, ActionKind: *kind, ActionArg: *arg, Enabled: true}
	}
	if len(pos) == 1 {
		a.Name = pos[0]
	} else if len(pos) > 1 || a.Name == "" {
		return errors.New(automationUsage)
	}
	if a.ActionKind != "create_task" && a.ActionKind != "comment" && a.ActionKind != "cli" {
		return fmt.Errorf("--kind must be create_task, comment or cli")
	}
	if err := automation.ValidSchedule(a.Schedule); err != nil {
		return err
	}
	if _, err := store.GetAutomation(s.DB, a.Name); err == nil {
		return fmt.Errorf("automation %q already exists", a.Name)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := store.CreateAutomation(s.DB, a); err != nil {
		return err
	}
	fmt.Fprintf(out, "added %s (%s)\n", a.Name, a.Schedule)
	return nil
}

func runImport(home string, args []string) error {
	if len(args) == 0 || args[0] != "jira" {
		return errors.New("usage: conveyor import jira --jql \"...\" --repo a")
	}
	fs := flag.NewFlagSet("import jira", flag.ContinueOnError)
	jql := fs.String("jql", "", "JQL query")
	repo := fs.String("repo", "", "repo for the new tasks")
	if _, err := parseArgs(fs, args[1:]); err != nil {
		return err
	}
	if *jql == "" || *repo == "" {
		return errors.New("usage: conveyor import jira --jql \"...\" --repo a")
	}
	return withService(home, func(s *core.Service) error {
		keys, err := jira.Import(context.Background(), s, *jql, *repo)
		for _, k := range keys {
			fmt.Fprintln(out, "created", k)
		}
		if err == nil {
			fmt.Fprintf(out, "imported %d\n", len(keys))
		}
		return err
	})
}
