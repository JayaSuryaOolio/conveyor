package store

import "fmt"

// Status is a tasks.status value (tech-spec §6).
type Status string

const (
	StatusBacklog   Status = "backlog"
	StatusTodo      Status = "todo"
	StatusRunning   Status = "running"
	StatusNeedsYou  Status = "needs_you"
	StatusReview    Status = "review"
	StatusDone      Status = "done"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// AllStatuses lists every status in board order.
var AllStatuses = []Status{StatusBacklog, StatusTodo, StatusRunning, StatusNeedsYou, StatusReview, StatusDone, StatusFailed, StatusCancelled}

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	for _, v := range AllStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// Actor says who requests a transition. Cancelling is human-only; ActorSystem
// covers the dispatcher, hooks, reaper, PR poller and system comments.
type Actor string

const (
	ActorHuman  Actor = "human"
	ActorSystem Actor = "system"
)

type actors uint8

const (
	byHuman actors = 1 << iota
	bySystem
	byAny = byHuman | bySystem
)

type edge struct{ from, to Status }

// transitions is the table from tech-spec §6: edge -> allowed actors.
var transitions = map[edge]actors{
	{StatusBacklog, StatusTodo}:     byHuman,
	{StatusTodo, StatusBacklog}:     byHuman,
	{StatusTodo, StatusRunning}:     bySystem,
	{StatusRunning, StatusNeedsYou}: bySystem,
	{StatusRunning, StatusReview}:   byAny, // system (hook) or human manual override
	{StatusRunning, StatusFailed}:   bySystem,
	{StatusRunning, StatusTodo}:     bySystem,
	{StatusNeedsYou, StatusRunning}: bySystem,
	{StatusNeedsYou, StatusTodo}:    byAny,
	{StatusNeedsYou, StatusDone}:    byHuman,
	{StatusReview, StatusTodo}:      byAny,
	{StatusReview, StatusNeedsYou}:  bySystem, // all PRs closed without merge
	{StatusReview, StatusDone}:      byAny,
	{StatusFailed, StatusTodo}:      byAny,
	{StatusDone, StatusTodo}:        byAny,
}

// ValidateTransition reports whether actor may move a task from -> to.
func ValidateTransition(from, to Status, actor Actor) error {
	if to == StatusCancelled {
		if from == StatusDone || from == StatusCancelled || !from.Valid() {
			return fmt.Errorf("illegal status transition: %s -> %s", from, to)
		}
		if actor != ActorHuman {
			return fmt.Errorf("status %q can only be set by a human", StatusCancelled)
		}
		return nil
	}
	allowed, ok := transitions[edge{from, to}]
	if !ok {
		return fmt.Errorf("illegal status transition: %s -> %s", from, to)
	}
	if (actor == ActorHuman && allowed&byHuman == 0) || (actor == ActorSystem && allowed&bySystem == 0) || (actor != ActorHuman && actor != ActorSystem) {
		return fmt.Errorf("status transition %s -> %s not allowed for actor %q", from, to, actor)
	}
	return nil
}
