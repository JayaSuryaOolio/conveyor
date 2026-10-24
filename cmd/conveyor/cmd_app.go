package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() {
	register("app", runApp, "open worktrees in external apps (list|presets|add|rm|open)")
	register("hooks", runHooks, "Claude Code hooks in ~/.claude/settings.json (status|install|uninstall)")
}

const appUsage = `usage: conveyor app list [--json] | presets [--json] | add NAME [--command CMD] | rm NAME
       conveyor app open NAME KEY|DIR [--repo r] [--print]
CMD is a shell command; {cwd} becomes the quoted worktree path. add without --command uses the preset of that name.`

func runApp(home string, args []string) error {
	if len(args) == 0 {
		return errors.New(appUsage)
	}
	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet("app "+cmd, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	command := fs.String("command", "", "shell command; {cwd} is the worktree path")
	repo := fs.String("repo", "", "repo of the task (default primary)")
	print := fs.Bool("print", false, "print the command instead of running it")
	pos, err := parseArgs(fs, rest)
	if err != nil {
		return err
	}
	if cmd == "presets" {
		ps := core.AppPresets()
		if *asJSON {
			return printJSON(ps)
		}
		return printApps(ps)
	}
	return withService(home, func(s *core.Service) error {
		switch cmd {
		case "list":
			as, err := store.ListApps(s.DB)
			if err != nil {
				return err
			}
			if *asJSON {
				return printJSON(as)
			}
			return printApps(as)
		case "add":
			if len(pos) != 1 {
				return errors.New(appUsage)
			}
			a := store.App{Name: pos[0], Command: *command}
			if a.Command == "" {
				for _, p := range core.AppPresets() {
					if p.Name == a.Name {
						a.Command = p.Command
					}
				}
				if a.Command == "" {
					return fmt.Errorf("no installed preset %q; pass --command (see conveyor app presets)", a.Name)
				}
			}
			if _, err := store.CreateApp(s.DB, a); err != nil {
				return err
			}
			fmt.Fprintf(out, "added %s: %s\n", a.Name, a.Command)
			return nil
		case "rm":
			if len(pos) != 1 {
				return errors.New(appUsage)
			}
			return store.DeleteApp(s.DB, pos[0])
		case "open":
			if len(pos) != 2 {
				return errors.New(appUsage)
			}
			a, err := store.GetApp(s.DB, pos[0])
			if err != nil {
				return err
			}
			cwd := pos[1]
			if fi, err := os.Stat(cwd); err == nil && fi.IsDir() {
				if cwd, err = filepath.Abs(cwd); err != nil {
					return err
				}
			} else if cwd, err = s.WorktreePath(pos[1], *repo); err != nil {
				return err
			}
			if *print {
				fmt.Fprintln(out, core.RenderApp(a.Command, cwd))
				return nil
			}
			return core.LaunchApp(a.Command, cwd)
		}
		return fmt.Errorf("unknown app command %q\n%s", cmd, appUsage)
	})
}

func printApps(as []store.App) error {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tCOMMAND")
	for _, a := range as {
		fmt.Fprintf(tw, "%s\t%s\n", a.Name, a.Command)
	}
	return tw.Flush()
}

func runHooks(home string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: conveyor hooks status|install|uninstall")
	}
	return withService(home, func(s *core.Service) error {
		st, err := s.UserHooks(args[0])
		if err == nil {
			fmt.Fprintln(out, st)
		}
		return err
	})
}
