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

// Link records "from <kind> to" (kind: blocks, relates).
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

// SetParent makes the task with key parent (empty: none) the parent of key. Keys don't change.
func (s *Service) SetParent(key, parent string) error {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return err
	}
	if parent == "" {
		return store.SetParent(s.DB, t.ID, nil)
	}
	p, err := store.GetTaskByKey(s.DB, parent)
	if err != nil {
		return err
	}
	return store.SetParent(s.DB, t.ID, &p.ID)
}
