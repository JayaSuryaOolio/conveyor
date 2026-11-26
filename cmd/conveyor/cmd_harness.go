package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/harness"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() {
	register("harness", runHarness, "harness checks: grade RUNNER GRADE | models [--json] | stale --current MODEL [--ledger FILE]")
}

func runHarness(home string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: conveyor harness grade RUNNER GRADE | models [--json] | stale --current MODEL [--ledger FILE]")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("harness "+sub, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	current := fs.String("current", "", "model name the ledger is compared against")
	ledger := fs.String("ledger", "docs/harness-ledger.md", "ledger file")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	switch sub {
	case "grade": // the model for a difficulty grade on a runner, for `task new --model`
		if len(pos) != 2 {
			return fmt.Errorf("usage: conveyor harness grade RUNNER GRADE (RUNNER claude|agy, GRADE easy|medium|hard)")
		}
		m, err := harness.ModelFor(pos[0], pos[1])
		if err != nil {
			return err
		}
		fmt.Fprintln(out, m)
	case "models": // outcomes of finished runs per (runner, model)
		return withService(home, func(s *core.Service) error {
			rs, err := store.AllRuns(s.DB)
			if err != nil {
				return err
			}
			stats := harness.ModelByOutcome(rs)
			if *asJSON {
				return printJSON(stats)
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "RUNNER\tMODEL\tRUNS\tDONE\tFAILED\tAVG_PEAK")
			for _, m := range stats {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\n", m.Runner, m.Model, m.Runs, m.Done, m.Failed, m.AvgPeak)
			}
			return tw.Flush()
		})
	case "stale": // ledger attempts written by an older model than --current
		if *current == "" {
			return fmt.Errorf("usage: conveyor harness stale --current MODEL [--ledger FILE]")
		}
		b, err := os.ReadFile(*ledger)
		if err != nil {
			return err
		}
		for _, id := range harness.StaleModelEntries(string(b), *current) {
			fmt.Fprintln(out, id)
		}
	default:
		return fmt.Errorf("unknown harness command %q", sub)
	}
	return nil
}
