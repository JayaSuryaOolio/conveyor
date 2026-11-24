package core

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// ParseDecisionID reads "D3" or "3".
func ParseDecisionID(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(s)), "D"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad decision id %q (want D<n>)", s)
	}
	return id, nil
}

// ApplyDecisionLines records a status block's decision, finding, dispute and undispute lines as the agent.
// Bad lines are skipped: an agent's typo must not fail its run.
func ApplyDecisionLines(db *sql.DB, taskID int64, decisions, findings, disputes, undisputes []string) {
	for _, d := range decisions {
		store.AddDecision(db, taskID, store.AuthorAgent, d)
	}
	for _, f := range findings {
		store.AddEntry(db, taskID, store.AuthorAgent, store.KindFinding, f)
	}
	for _, l := range disputes {
		ref, reason, _ := strings.Cut(l, "|")
		if id, err := ParseDecisionID(ref); err == nil {
			store.SetDispute(db, taskID, id, store.AuthorAgent, reason)
		}
	}
	for _, l := range undisputes {
		if id, err := ParseDecisionID(l); err == nil {
			store.ClearDispute(db, taskID, id)
		}
	}
}

// Decisions lists a task's decisions.
func (s *Service) Decisions(key string) ([]store.Decision, error) {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return nil, err
	}
	return store.ListDecisions(s.DB, t.ID)
}

// AddDecision records a decision stated by the human.
func (s *Service) AddDecision(key, body string) error {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return err
	}
	_, err = store.AddDecision(s.DB, t.ID, store.AuthorHuman, body)
	return err
}

// RemoveDecision strikes decision id from the task.
func (s *Service) RemoveDecision(key string, id int64) error {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return err
	}
	return store.RemoveDecision(s.DB, t.ID, id)
}

// DisputeDecision marks decision id disputed by the human; an empty reason unmarks it.
func (s *Service) DisputeDecision(key string, id int64, reason string) error {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return store.ClearDispute(s.DB, t.ID, id)
	}
	return store.SetDispute(s.DB, t.ID, id, store.AuthorHuman, reason)
}
