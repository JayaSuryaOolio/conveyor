package core

import (
	"slices"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// HasOpenEpic is true while an unfinished epic task (label `epic` or `epic:<name>`) exists.
func (s *Service) HasOpenEpic() bool {
	ts, _ := store.ListTasks(s.DB)
	return slices.ContainsFunc(ts, func(t store.Task) bool {
		return t.Status != store.StatusDone && t.Status != store.StatusCancelled &&
			slices.ContainsFunc(t.Labels, func(l string) bool { return l == "epic" || strings.HasPrefix(l, "epic:") })
	})
}
