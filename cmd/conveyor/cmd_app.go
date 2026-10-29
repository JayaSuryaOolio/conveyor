package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() {
	register("app", runApp, "open worktrees in external apps, run quick actions (list|presets|add|install|export|convert|run|global|output|rm|open)")
	register("hooks", runHooks, "Claude Code hooks in ~/.claude/settings.json (status|install|uninstall)")
}

const appUsage = `usage: conveyor app list [--json] | presets [--json] | add NAME [--command CMD] [--global] [--output]
       conveyor app install FILE|- [--replace] (manifest JSON; - reads stdin) | export NAME [FILE] | convert NAME | run APP ACTION [KEY] [--place p]
       conveyor app global|output NAME on|off | rm NAME
       conveyor app open NAME [KEY|DIR] [--repo r] [--print]
CMD is a shell command; {cwd} becomes the quoted worktree path and {key} the task key ('' without one).
add without --command uses the preset of that name.
A global app is not tied to a worktree: it shows in the UI nav bar instead of on task pages, and open NAME
(no KEY|DIR) runs it with {cwd} = your home directory.
An output app is a quick action: it runs to completion in {cwd} and its output is shown (a toast in the app).
Scripts can use conveyor task sessions KEY [--json] for the task's Claude session ids.`

func runApp(home string, args []string) error {
	if len(args) == 0 {
		return errors.New(appUsage)
	}
	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet("app "+cmd, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	command := fs.String("command", "", "shell command; {cwd} is the worktree path")
	global := fs.Bool("global", false, "app is not tied to a worktree")
	output := fs.Bool("output", false, "run to completion and show the output (a quick action)")
	replace := fs.Bool("replace", false, "install over an app of the same name")
	repo := fs.String("repo", "", "repo of the task (default primary)")
	print := fs.Bool("print", false, "print the command instead of running it")
	place := fs.String("place", "", "run the action as shown at this place (its service there may override run)")
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
			a := store.App{Name: pos[0], Command: *command, Global: *global, Output: *output}
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
		case "global", "output":
			if len(pos) != 2 || pos[1] != "on" && pos[1] != "off" {
				return errors.New(appUsage)
			}
			a, err := store.GetApp(s.DB, pos[0])
			if err != nil {
				return err
			}
			if cmd == "global" {
				a.Global = pos[1] == "on"
			} else {
				a.Output = pos[1] == "on"
			}
			return store.UpdateApp(s.DB, a)
		case "rm":
			if len(pos) != 1 {
				return errors.New(appUsage)
			}
			return s.UninstallApp(pos[0])
		case "convert": // plain command app -> manifest app
			if len(pos) != 1 {
				return errors.New(appUsage)
			}
			return s.ConvertApp(pos[0])
		case "install":
			if len(pos) != 1 {
				return errors.New(appUsage)
			}
			var b []byte
			var err error
			if pos[0] == "-" {
				b, err = io.ReadAll(os.Stdin)
			} else {
				b, err = os.ReadFile(pos[0])
			}
			if err != nil {
				return err
			}
			a, err := s.InstallManifest(string(b), *replace)
			if err == nil {
				fmt.Fprintf(out, "installed %s\n", a.Name)
			}
			return err
		case "export": // manifest app -> JSON on stdout or FILE
			if len(pos) < 1 || len(pos) > 2 {
				return errors.New(appUsage)
			}
			raw, err := s.ExportApp(pos[0])
			if err != nil {
				return err
			}
			if len(pos) == 2 {
				return os.WriteFile(pos[1], []byte(raw+"\n"), 0o644)
			}
			fmt.Fprintln(out, raw)
			return nil
		case "run":
			if len(pos) < 2 || len(pos) > 3 {
				return errors.New(appUsage)
			}
			res, err := s.RunAppAction(pos[0], pos[1], *place, strings.Join(pos[2:], ""))
			if res != "" {
				fmt.Fprintln(out, res)
			}
			return err
		case "open":
			if len(pos) < 1 || len(pos) > 2 {
				return errors.New(appUsage)
			}
			a, err := store.GetApp(s.DB, pos[0])
			if err != nil {
				return err
			}
			cwd, key, err := appCwd(s, a, pos[1:], *repo)
			if err != nil {
				return err
			}
			if *print {
				fmt.Fprintln(out, core.RenderApp(a.Command, cwd, key))
				return nil
			}
			res, err := core.RunApp(a, cwd, key)
			if res != "" {
				fmt.Fprintln(out, res)
			}
			return err
		}
		return fmt.Errorf("unknown app command %q\n%s", cmd, appUsage)
	})
}

// appCwd resolves where an app opens: a directory, a task key's worktree, or (global apps, no arg) home.
// key is the task key when one was given.
func appCwd(s *core.Service, a store.App, arg []string, repo string) (cwd, key string, err error) {
	if len(arg) == 0 {
		if !a.Global {
			return "", "", fmt.Errorf("%s is not a global app; pass a task KEY or DIR", a.Name)
		}
		cwd, err = os.UserHomeDir()
		return cwd, "", err
	}
	if fi, err := os.Stat(arg[0]); err == nil && fi.IsDir() {
		cwd, err = filepath.Abs(arg[0])
		return cwd, "", err
	}
	cwd, err = s.WorktreePath(arg[0], repo)
	return cwd, arg[0], err
}

func printApps(as []store.App) error {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tGLOBAL\tOUTPUT\tCOMMAND")
	for _, a := range as {
		if a.Manifest != "" {
			a.Command = "(manifest)"
		}
		fmt.Fprintf(tw, "%s\t%v\t%v\t%s\n", a.Name, a.Global, a.Output, a.Command)
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
