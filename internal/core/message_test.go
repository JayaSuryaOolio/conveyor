package core

import (
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// replyPair makes two todo tasks and has a message b --reply.
func replyPair(t *testing.T, s *Service) (a, b store.Task) {
	t.Helper()
	a, _ = s.CreateTask(NewTaskInput{Title: "a", Repos: []string{"a"}, Start: true})
	b, _ = s.CreateTask(NewTaskInput{Title: "b", Repos: []string{"a"}, Start: true})
	if _, err := s.Message(a.Key, b.Key, "review my PR", FollowUp, true); err != nil {
		t.Fatal(err)
	}
	return a, b
}

// readAndEnd has a run of b consume its inbox, then ends it with the given run fields.
func readAndEnd(t *testing.T, s *Service, b store.Task, end func(*store.Run)) {
	t.Helper()
	r, err := store.CreateRun(s.DB, store.Run{TaskID: b.ID, Kind: "launch", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	cs, _ := store.ListComments(s.DB, b.ID)
	var ids []int64
	for _, c := range cs {
		ids = append(ids, c.ID)
	}
	if err := store.ConsumeComments(s.DB, ids, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.TickReplies(); err != nil { // run still going: nothing to send
		t.Fatal(err)
	}
	if got := fromTask(t, s, b.Key); len(got) != 0 {
		t.Fatalf("reply before the run ended: %v", got)
	}
	ended := "2026-01-01T00:00:00Z"
	r.EndedAt = &ended
	end(&r)
	if err := store.UpdateRun(s.DB, r); err != nil {
		t.Fatal(err)
	}
}

// fromTask lists the bodies of comments sent by task key, on any task.
func fromTask(t *testing.T, s *Service, key string) []string {
	t.Helper()
	rows, err := s.DB.Query(`SELECT body, wants_reply FROM comments WHERE source=?`, SourceTask+key)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var body string
		var wants bool
		rows.Scan(&body, &wants)
		if wants {
			t.Errorf("a reply asked for a reply: %q", body)
		}
		out = append(out, body)
	}
	return out
}


func TestReplyDeliveredOnceAfterRunEnds(t *testing.T) {
	s := newSvc(t)
	a, b := replyPair(t, s)
	readAndEnd(t, s, b, func(r *store.Run) {
		r.Status, r.BlockStatus, r.FinalMessage = "finished", sp("done"), sp("LGTM, one nit")
	})
	for i := 0; i < 2; i++ { // the second tick must not send it again
		if err := s.TickReplies(); err != nil {
			t.Fatal(err)
		}
	}
	got := fromTask(t, s, b.Key)
	if len(got) != 1 || got[0] != "reply from "+b.Key+":\n\nLGTM, one nit" {
		t.Fatalf("replies %q", got)
	}
	cs, _ := store.ListComments(s.DB, a.ID)
	if len(cs) != 1 || cs[0].Source != SourceTask+b.Key {
		t.Fatalf("a's thread %+v", cs)
	}
}

func TestFailedRunSendsStopReply(t *testing.T) {
	s := newSvc(t)
	_, b := replyPair(t, s)
	readAndEnd(t, s, b, func(r *store.Run) { r.Status, r.Error = "failed", sp("session crashed\nstack") })
	if err := s.TickReplies(); err != nil {
		t.Fatal(err)
	}
	got := fromTask(t, s, b.Key)
	if len(got) != 1 || got[0] != b.Key+" stopped: failed – session crashed" {
		t.Fatalf("replies %q", got)
	}
}

func TestReplyCap(t *testing.T) {
	s := newSvc(t)
	a, b := replyPair(t, s)
	if _, err := s.Message(b.Key, a.Key, "q2", FollowUp, true); err != nil { // either direction counts
		t.Fatal(err)
	}
	if _, err := s.Message(a.Key, b.Key, "q3", FollowUp, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Message(a.Key, b.Key, "plain", FollowUp, false); err != nil { // no --reply: not capped
		t.Fatal(err)
	}
	_, err := s.Message(b.Key, a.Key, "q4", FollowUp, true)
	if err == nil || !strings.Contains(err.Error(), "a human comments") {
		t.Fatalf("4th --reply: %v", err)
	}
	if _, err := s.AddComment(b.Key, store.AuthorHuman, "carry on", FollowUp); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Message(b.Key, a.Key, "q4", FollowUp, true); err != nil {
		t.Fatalf("after a human comment: %v", err)
	}
}

func TestMessageRefusesBacklog(t *testing.T) {
	s := newSvc(t)
	a, _ := s.CreateTask(NewTaskInput{Title: "a", Repos: []string{"a"}, Start: true})
	b, _ := s.CreateTask(NewTaskInput{Title: "b", Repos: []string{"a"}})
	if _, err := s.Message(a.Key, b.Key, "hi", FollowUp, true); err == nil {
		t.Fatal("backlog target accepted")
	}
}
