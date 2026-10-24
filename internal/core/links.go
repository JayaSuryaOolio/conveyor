package core

import "github.com/JayaSuryaOolio/conveyor/internal/store"

// pair resolves two task keys to ids.
func (s *Service) pair(a, b string) (int64, int64, error) {
	ta, err := store.GetTaskByKey(s.DB, a)
	if err != nil {
		return 0, 0, err
	}
	tb, err := store.GetTaskByKey(s.DB, b)
	if err != nil {
		return 0, 0, err
	}
	return ta.ID, tb.ID, nil
}

// Link records "from <kind> to" (kind: blocks, relates, duplicates).
func (s *Service) Link(from, kind, to string) error {
	a, b, err := s.pair(from, to)
	if err != nil {
		return err
	}
	return store.AddLink(s.DB, a, b, kind)
}

// Unlink removes "from <kind> to".
func (s *Service) Unlink(from, kind, to string) error {
	a, b, err := s.pair(from, to)
	if err != nil {
		return err
	}
	return store.RemoveLink(s.DB, a, b, kind)
}
