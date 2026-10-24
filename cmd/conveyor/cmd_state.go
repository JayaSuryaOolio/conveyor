package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
)

func init() {
	register("state", runState, "export or import settings, apps, automations and user agents as JSON")
}

const stateUsage = "usage: conveyor state export [FILE] | import FILE [--repo A=name ...]"

type repoFlags map[string]string

func (r repoFlags) String() string { return "" }
func (r repoFlags) Set(v string) error {
	p, name, ok := strings.Cut(v, "=")
	if !ok || p == "" || name == "" {
		return fmt.Errorf("--repo wants PLACEHOLDER=name, got %q", v)
	}
	r[p] = name
	return nil
}

func runState(home string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(stateUsage)
	}
	return withService(home, func(s *core.Service) error {
		switch args[0] {
		case "export":
			if len(args) > 2 {
				return fmt.Errorf(stateUsage)
			}
			st, err := s.ExportState()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				return printJSON(st)
			}
			b, err := json.MarshalIndent(st, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(args[1], append(b, '\n'), 0o600); err != nil {
				return err
			}
			fmt.Fprintf(out, "exported %d settings, %d apps, %d automations, %d agents to %s\n", len(st.Settings), len(st.Apps), len(st.Automations), len(st.Agents), args[1])
			if len(st.Repos) > 0 {
				fmt.Fprintf(out, "automations use repo placeholders %s; map them on import with --repo %s=<name>\n", strings.Join(st.Repos, ","), st.Repos[0])
			}
			return nil
		case "import":
			fs := flag.NewFlagSet("state import", flag.ContinueOnError)
			repos := repoFlags{}
			fs.Var(repos, "repo", "map a placeholder to a registered repo: A=name (repeatable)")
			pos, err := parseArgs(fs, args[1:])
			if err != nil {
				return err
			}
			if len(pos) != 1 {
				return fmt.Errorf(stateUsage)
			}
			b, err := os.ReadFile(pos[0])
			if err != nil {
				return err
			}
			var st core.State
			if err := json.Unmarshal(b, &st); err != nil {
				return fmt.Errorf("%s: %w", pos[0], err)
			}
			lines, err := s.ImportState(st, repos)
			for _, l := range lines {
				fmt.Fprintln(out, l)
			}
			return err
		}
		return fmt.Errorf(stateUsage)
	})
}
