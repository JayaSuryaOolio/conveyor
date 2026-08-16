package store

import "fmt"

// Status is a kanban_tasks.status value (PRD Section 4).
type Status string

const (
	StatusDraft              Status = "draft"
	StatusGrooming           Status = "grooming"
	StatusTodo               Status = "todo"
	StatusInProgress         Status = "in_progress"
	StatusNeedsClarification Status = "needs_clarification"
	StatusDone               Status = "done"
	StatusFailed             Status = "failed"
	StatusCancelled          Status = "cancelled"
	StatusArchived           Status = "archived"
)

// Actor identifies who is requesting a status transition, since PRD Section 4
// makes cancelled a human-only transition ("Human only — never AI").
type Actor string

const (
	ActorHuman  Actor = "human"
	ActorSystem Actor = "system"
)

// transitions is the directed edge set of the state diagram in PRD Section 4.
// It only says which edges exist; actor eligibility is layered on top in
// ValidateTransition, since not every legal edge is legal for every actor.
var transitions = map[Status]map[Status]bool{
	StatusDraft: {
		StatusGrooming:  true,
		StatusCancelled: true,
	},
	StatusGrooming: {
		StatusTodo:      true,
		StatusCancelled: true,
	},
	StatusTodo: {
		StatusInProgress: true,
		StatusCancelled:  true,
	},
	StatusInProgress: {
		StatusNeedsClarification: true,
		StatusDone:               true,
		StatusFailed:             true,
		StatusCancelled:          true,
	},
	StatusNeedsClarification: {
		StatusInProgress: true,
		StatusFailed:     true,
		StatusCancelled:  true,
	},
	StatusFailed: {
		StatusTodo:     true,
		StatusArchived: true,
	},
	StatusDone: {
		StatusArchived: true,
	},
	StatusCancelled: {
		StatusArchived: true,
	},
}

// ValidateTransition reports whether moving a task from `from` to `to` is
// legal for the given actor. Entering `cancelled` is human-only regardless of
// the edge table; entering `archived` allows either actor since the PRD's own
// retention sweep is a legitimate automatic path there.
func ValidateTransition(from, to Status, actor Actor) error {
	if to == StatusCancelled && actor != ActorHuman {
		return fmt.Errorf("status %q can only be set by a human", StatusCancelled)
	}
	if allowed, ok := transitions[from]; !ok || !allowed[to] {
		return fmt.Errorf("illegal status transition: %s -> %s", from, to)
	}
	return nil
}
