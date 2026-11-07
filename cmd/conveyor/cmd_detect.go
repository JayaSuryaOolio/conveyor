package main

import (
	"flag"
	"fmt"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/runner"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() {
	register("detect", runDetect, "detect available agent runners and show active configuration")
}

func runDetect(home string, args []string) error {
	return withService(home, func(s *core.Service) error {
		fs := flag.NewFlagSet("detect", flag.ContinueOnError)
		fix := fs.Bool("fix", false, "auto-configure default_runner if missing or not installed")
		pos, err := parseArgs(fs, args)
		if err != nil {
			return err
		}
		if len(pos) > 0 {
			return fmt.Errorf("detect takes no positional arguments")
		}

		agents := runner.DetectRunners()
		curRunner := store.GetSetting(s.DB, "default_runner")

		fmt.Fprintln(out, "DETECTED AGENT RUNNERS:")
		for _, a := range agents {
			status := "not installed"
			if a.Installed {
				status = fmt.Sprintf("installed (%s)", a.Path)
			}
			active := ""
			if a.ID == curRunner {
				active = " [active default]"
			}
			fmt.Fprintf(out, "  %-12s (%s): %s%s\n", a.Name, a.Command, status, active)
		}

		if *fix {
			picked, changed, err := runner.AutoConfigureDefaultRunner(s.DB)
			if err != nil {
				return fmt.Errorf("auto-configure default_runner: %w", err)
			}
			if changed {
				fmt.Fprintf(out, "\nUpdated default_runner to %s\n", picked)
			} else {
				fmt.Fprintf(out, "\ndefault_runner is already optimal (%s)\n", picked)
			}
		} else {
			fmt.Fprintf(out, "\nCurrent default_runner: %s\n", curRunner)
		}

		return nil
	})
}
