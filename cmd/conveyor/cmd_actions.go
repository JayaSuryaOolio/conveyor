package main

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
)

func init() {
	register("actions", runActions, "list what an external tool can run, with argv ([KEY|@] [--json])")
}

func runActions(home string, args []string) error {
	fs := flag.NewFlagSet("actions", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return errors.New("usage: conveyor actions [KEY|@] [--json]")
	}
	key := strings.Join(pos, "")
	return withService(home, func(s *core.Service) error {
		as, err := s.ControlActions(key)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(as)
		}
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, a := range as {
			c := ""
			if a.Confirm != "" {
				c = "confirm: " + a.Confirm
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Label, a.Kind, strings.Join(a.Argv, " "), c)
		}
		return tw.Flush()
	})
}
