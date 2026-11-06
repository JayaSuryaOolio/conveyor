package jira

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// pollEvery is how often the poller checks which sources are due.
const pollEvery = 30 * time.Second

// GetSource returns the epic's source, or nil when none is attached.
func GetSource(svc *core.Service, epicKey string) (*store.JiraSource, error) {
	epic, err := store.GetTaskByKey(svc.DB, epicKey)
	if err != nil {
		return nil, err
	}
	s, err := store.GetJiraSource(svc.DB, epic.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// SetSource attaches or reconfigures the epic's source. The Jira epic must be assigned to the acli user.
func SetSource(svc *core.Service, epicKey string, s store.JiraSource) error {
	epic, err := store.GetTaskByKey(svc.DB, epicKey)
	if err != nil {
		return err
	}
	var m map[string]string
	if s.StatusMap != "" && json.Unmarshal([]byte(s.StatusMap), &m) != nil {
		return errors.New(`status map must be a JSON object like {"review":"In Review"}`)
	}
	if !store.ValidJiraKey(s.JiraKey) { // checked before the key goes into JQL
		return fmt.Errorf("invalid jira key %q (want PROJ-123)", s.JiraKey)
	}
	mine, err := search(context.Background(), "key = "+s.JiraKey+" AND assignee = currentUser()", "key")
	if err != nil {
		return err
	}
	if len(mine) == 0 {
		return fmt.Errorf("jira epic %s is not assigned to the acli user (or does not exist)", s.JiraKey)
	}
	s.EpicID = epic.ID
	return store.SetJiraSource(svc.DB, s)
}

// ClearSource detaches the epic's source; synced tasks stay.
func ClearSource(svc *core.Service, epicKey string) error {
	epic, err := store.GetTaskByKey(svc.DB, epicKey)
	if err != nil {
		return err
	}
	return store.ClearJiraSource(svc.DB, epic.ID)
}

// Poller pulls every source whose interval has elapsed. Pull errors land on the source, never here.
type Poller struct {
	tried map[int64]time.Time // last attempt: a failing source (last_sync does not advance) retries per interval, not per tick
}

// Tick syncs the due sources once.
func (p *Poller) Tick(ctx context.Context, svc *core.Service, now time.Time) {
	if p.tried == nil {
		p.tried = map[int64]time.Time{}
	}
	srcs, err := store.ListJiraSources(svc.DB)
	if err != nil {
		log.Printf("jira poll: %v", err)
		return
	}
	for _, s := range srcs {
		iv := time.Duration(s.IntervalSec) * time.Second
		if s.Done || now.Sub(p.tried[s.EpicID]) < iv {
			continue
		}
		if s.LastSync != nil {
			if lt, err := time.Parse(time.RFC3339, *s.LastSync); err == nil && now.Sub(lt) < iv {
				continue
			}
		}
		p.tried[s.EpicID] = now
		epic, err := store.GetTask(svc.DB, s.EpicID)
		if err != nil {
			continue
		}
		if _, err := Pull(ctx, svc, epic.Key); err != nil {
			log.Printf("jira poll %s: %v", epic.Key, err)
		}
	}
}

// Run ticks until ctx ends.
func (p *Poller) Run(ctx context.Context, svc *core.Service) {
	for {
		p.Tick(ctx, svc, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollEvery):
		}
	}
}
