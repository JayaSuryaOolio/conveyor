package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/gh"
)

func init() {
	register("epic", runEpic, "epic helpers (review-check TASK [--step N])")
}

func runEpic(home string, args []string) error {
	if len(args) == 0 || args[0] != "review-check" {
		return errors.New("usage: conveyor epic review-check TASK [--step N]")
	}
	fs := flag.NewFlagSet("epic review-check", flag.ContinueOnError)
	step := fs.Int("step", 0, "empty checks so far (backoff position)")
	pos, err := parseArgs(fs, args[1:])
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: conveyor epic review-check TASK [--step N]")
	}
	return withService(home, func(s *core.Service) error {
		res, err := gh.ReviewCheck(context.Background(), s, pos[0], *step)
		if err == nil {
			fmt.Fprintln(out, res)
		}
		return err
	})
}
