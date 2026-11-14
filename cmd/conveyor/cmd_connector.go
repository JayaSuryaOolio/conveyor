package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/JayaSuryaOolio/conveyor/internal/connectors"
	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() {
	register("connector", runConnector, "open worktrees in external tools, run quick actions (list|presets|add|install|export|run|enable|disable|rm|open)")
	register("app", runConnector, "alias of connector")
	register("plugin", runPlugin, "Conveyor plugin, hooks + skills, per tool (status|install|uninstall [tool])")
	register("hooks", runPlugin, "alias of plugin")
}

const connectorUsage = `usage: conveyor connector list [--json] | presets [--json] | add NAME [--command CMD] [--output] | add --from-app NAME [--replace]
       conveyor connector install FILE|- [--replace] (manifest JSON; - reads stdin) | export NAME [FILE] | run CONNECTOR ACTION [KEY] [--place p]
       conveyor connector enable NAME | disable NAME | rm NAME
       conveyor connector open NAME KEY [--repo r]
add --command installs CMD, a shell command, as a manifest connector: {cwd} becomes ${{ worktree.path }} and {key} ${{ task.key }}.
add without --command uses the preset of that name.
An --output connector is a quick action: it runs to completion in the worktree and its output is shown (a toast in the app).
Scripts can use conveyor task sessions KEY [--json] for the task's Claude session ids.`

func runConnector(home string, args []string) error {
	if len(args) == 0 {
		return errors.New(connectorUsage)
	}
	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet("connector "+cmd, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	command := fs.String("command", "", "shell command; {cwd} is the worktree path")
	output := fs.Bool("output", false, "run to completion and show the output (a quick action)")
	replace := fs.Bool("replace", false, "install over a connector of the same name")
	fromApp := fs.String("from-app", "", "add: build a manifest connector from an installed .app (e.g. \"Visual Studio Code\")")
	repo := fs.String("repo", "", "repo of the task (default primary)")
	place := fs.String("place", "", "run the action as shown at this place (its service there may override run)")
	pos, err := parseArgs(fs, rest)
	if err != nil {
		return err
	}
	if cmd == "presets" {
		ps := connectors.ConnectorPresets()
		if *asJSON {
			return printJSON(ps)
		}
		return printConnectors(ps)
	}
	return withService(home, func(s *core.Service) error {
		switch cmd {
		case "list":
			as, err := store.ListConnectors(s.DB)
			if err != nil {
				return err
			}
			if *asJSON {
				return printJSON(as)
			}
			return printConnectors(as)
		case "add":
			if len(pos) != 1 && *fromApp == "" {
				return errors.New(connectorUsage)
			}
			if *fromApp != "" {
				raw, err := connectors.ManifestFromApp(*fromApp)
				if err != nil {
					return err
				}
				a, err := s.InstallManifest(raw, *replace)
				if err == nil {
					fmt.Fprintf(out, "installed %s\n", a.Name)
				}
				return err
			}
			name := pos[0]
			if *command != "" {
				a, err := s.InstallCommand(name, *command, *output)
				if err == nil {
					fmt.Fprintf(out, "added %s\n", a.Name)
				}
				return err
			}
			for _, p := range connectors.ConnectorPresets() {
				if p.Name == name {
					a, err := s.InstallManifest(p.Manifest, false)
					if err == nil {
						fmt.Fprintf(out, "added %s\n", a.Name)
					}
					return err
				}
			}
			return fmt.Errorf("no installed preset %q; pass --command (see conveyor app presets)", name)
		case "enable", "disable":
			if len(pos) != 1 {
				return errors.New(connectorUsage)
			}
			return store.SetConnectorDisabled(s.DB, pos[0], cmd == "disable")
		case "rm":
			if len(pos) != 1 {
				return errors.New(connectorUsage)
			}
			return s.UninstallConnector(pos[0])
		case "install":
			if len(pos) != 1 {
				return errors.New(connectorUsage)
			}
			m, err := readText(pos[0])
			if err != nil {
				return err
			}
			a, err := s.InstallManifest(m, *replace)
			if err == nil {
				fmt.Fprintf(out, "installed %s\n", a.Name)
			}
			return err
		case "export": // connector -> JSON on stdout or FILE
			if len(pos) < 1 || len(pos) > 2 {
				return errors.New(connectorUsage)
			}
			raw, err := s.ExportConnector(pos[0])
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
				return errors.New(connectorUsage)
			}
			res, err := s.RunConnectorAction(pos[0], pos[1], *place, strings.Join(pos[2:], ""))
			if res != "" {
				fmt.Fprintln(out, res)
			}
			return err
		case "open":
			if len(pos) != 2 {
				return errors.New(connectorUsage)
			}
			res, err := s.OpenConnector(pos[0], pos[1], *repo)
			if res != "" {
				fmt.Fprintln(out, res)
			}
			return err
		}
		return fmt.Errorf("unknown connector command %q\n%s", cmd, connectorUsage)
	})
}

func printConnectors(as []store.Connector) error {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tACTIONS")
	for _, a := range as {
		var acts []string
		if m, err := connectors.ParseManifest(a.Manifest); err == nil {
			for _, x := range m.Actions {
				acts = append(acts, x.ID)
			}
		}
		name := a.Name
		if a.Disabled {
			name += " (disabled)"
		}
		fmt.Fprintf(tw, "%s\t%s\n", name, strings.Join(acts, ","))
	}
	return tw.Flush()
}

func runPlugin(home string, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: conveyor plugin status|install|uninstall [claude|agy]")
	}
	tool := ""
	if len(args) == 2 {
		tool = args[1]
	}
	return withService(home, func(s *core.Service) error {
		sts, err := s.Plugin(tool, args[0])
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, st := range sts {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", st.Tool, st.Status, st.Note)
		}
		tw.Flush()
		return err
	})
}
