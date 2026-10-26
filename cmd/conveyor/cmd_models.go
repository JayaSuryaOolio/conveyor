package main

import (
	"flag"
	"fmt"
	"text/tabwriter"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
)

func init() {
	register("models", runModels, "Claude models detected at runtime (API or recent transcripts)")
}

func runModels(home string, args []string) error {
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	refresh := fs.Bool("refresh", false, "re-detect instead of using the 1h cache")
	asJSON := fs.Bool("json", false, "JSON output")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	return withService(home, func(s *core.Service) error {
		ml := s.Models(*refresh)
		if *asJSON {
			return printJSON(ml)
		}
		if ml.Err != "" {
			return fmt.Errorf("model detection failed (%s): %s", ml.Source, ml.Err)
		}
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tTIER\tALIAS")
		for _, m := range ml.Models {
			alias := ""
			if m.Latest {
				alias = m.Tier
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", m.ID, m.Tier, alias)
		}
		fmt.Fprintf(tw, "(from %s, %s)\n", ml.Source, ml.At.Format("15:04"))
		return tw.Flush()
	})
}
