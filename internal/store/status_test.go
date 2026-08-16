package store

import "testing"

func TestValidateTransition_ValidEdges(t *testing.T) {
	cases := []struct {
		from, to Status
		actor    Actor
	}{
		{StatusDraft, StatusGrooming, ActorHuman},
		{StatusGrooming, StatusTodo, ActorHuman},
		{StatusTodo, StatusInProgress, ActorSystem},
		{StatusInProgress, StatusNeedsClarification, ActorSystem},
		{StatusInProgress, StatusDone, ActorSystem},
		{StatusInProgress, StatusFailed, ActorSystem},
		{StatusNeedsClarification, StatusInProgress, ActorSystem},
		{StatusNeedsClarification, StatusFailed, ActorSystem},
		{StatusFailed, StatusTodo, ActorHuman},
		{StatusDone, StatusArchived, ActorHuman},
		{StatusDone, StatusArchived, ActorSystem},
		{StatusFailed, StatusArchived, ActorSystem},
		{StatusCancelled, StatusArchived, ActorSystem},
		{StatusDraft, StatusCancelled, ActorHuman},
		{StatusGrooming, StatusCancelled, ActorHuman},
		{StatusTodo, StatusCancelled, ActorHuman},
		{StatusInProgress, StatusCancelled, ActorHuman},
		{StatusNeedsClarification, StatusCancelled, ActorHuman},
	}
	for _, c := range cases {
		if err := ValidateTransition(c.from, c.to, c.actor); err != nil {
			t.Errorf("expected %s -> %s (actor=%s) to be valid, got error: %v", c.from, c.to, c.actor, err)
		}
	}
}

func TestValidateTransition_InvalidEdges(t *testing.T) {
	cases := []struct {
		from, to Status
		actor    Actor
	}{
		{StatusDone, StatusInProgress, ActorSystem},
		{StatusDraft, StatusDone, ActorHuman},
		{StatusCancelled, StatusTodo, ActorHuman},
		{StatusArchived, StatusTodo, ActorHuman},
		{StatusTodo, StatusDone, ActorSystem},
		{StatusGrooming, StatusInProgress, ActorSystem},
	}
	for _, c := range cases {
		if err := ValidateTransition(c.from, c.to, c.actor); err == nil {
			t.Errorf("expected %s -> %s (actor=%s) to be invalid, got no error", c.from, c.to, c.actor)
		}
	}
}

func TestValidateTransition_CancelledIsHumanOnly(t *testing.T) {
	// draft -> cancelled is a legal edge, but only for a human actor.
	if err := ValidateTransition(StatusDraft, StatusCancelled, ActorSystem); err == nil {
		t.Error("expected system actor to be rejected from entering cancelled, got no error")
	}
	if err := ValidateTransition(StatusInProgress, StatusCancelled, ActorSystem); err == nil {
		t.Error("expected system actor to be rejected from entering cancelled, got no error")
	}
}

func TestValidateTransition_ArchivedAllowsEitherActor(t *testing.T) {
	if err := ValidateTransition(StatusDone, StatusArchived, ActorHuman); err != nil {
		t.Errorf("expected human to archive a done task, got error: %v", err)
	}
	if err := ValidateTransition(StatusDone, StatusArchived, ActorSystem); err != nil {
		t.Errorf("expected system sweep to archive a done task, got error: %v", err)
	}
}
