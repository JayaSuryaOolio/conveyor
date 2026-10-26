package main

import (
	"flag"
	"fmt"
	"text/tabwriter"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
)

func init() {
	register("models", runModels, "Claude models (Anthropic API with ANTHROPIC_API_KEY, else a built-in list)")
}

func runModels(home string, args []string) error {
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	return withService(home, func(s *core.Service) error {
		ml := s.Models()
		if *asJSON {
			return printJSON(ml)
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
		fmt.Fprintf(tw, "(source: %s)\n", ml.Source)
		return tw.Flush()
	})
}
