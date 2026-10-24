package main

import (
	"fmt"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
)

func init() {
	register("mock", runMock, "seed an empty home with mock repos, one task per board scenario, settings, apps and automations")
}

func runMock(home string, args []string) error {
	if len(args) != 1 || args[0] != "seed" {
		return fmt.Errorf("usage: conveyor --home <empty dir> mock seed")
	}
	return withService(home, func(s *core.Service) error {
		if err := s.SeedMock(home); err != nil {
			return err
		}
		fmt.Fprintln(out, "seeded mock repos, tasks, settings, apps and automations in", home)
		return nil
	})
}
