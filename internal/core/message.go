package core

import (
	"fmt"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// SourceTask is the comment source of a message from another task: "task:CV-5". The thread shows it
// as "system (via task:CV-5)", so a reader can tell a task's message from the app's own.
const SourceTask = "task:"

// Message sends body from task `from` to task `to`. It is a comment in the target's inbox (the same
// queue humans use: delivered by the next run, `consumed_by_run_id`), authored system with the sender
// as source, so it never counts as a human stepping in.
//
// The result says what it did, so a sender is never left guessing:
//   - target running or todo: queued, read by its next run
//   - target in review, needs_you, failed or done: requeued
//   - target backlog or cancelled: refused (only a human starts those), nothing is stored
func (s *Service) Message(from, to, body string, after AfterMerge) (string, error) {
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("message is empty")
	}
	f, err := store.GetTaskByKey(s.DB, from)
	if err != nil {
		return "", err
	}
	t, err := store.GetTaskByKey(s.DB, to)
	if err != nil {
		return "", err
	}
	if f.ID == t.ID {
		return "", fmt.Errorf("a task cannot message itself")
	}
	switch t.Status {
	case store.StatusBacklog, store.StatusCancelled:
		return "", fmt.Errorf("%s is %s and nothing starts it: a human moves it to todo first", t.Key, t.Status)
	}
	if _, err := s.AddCommentVia(t.Key, store.AuthorSystem, SourceTask+f.Key, body, after); err != nil {
		return "", err
	}
	now, _ := store.GetTask(s.DB, t.ID) // the status the comment left it in
	res := fmt.Sprintf("%s: queued, it reads it with its next run", t.Key)
	if t.Status != store.StatusRunning && t.Status != store.StatusTodo {
		res = fmt.Sprintf("%s: requeued from %s, starts on the next dispatcher tick", t.Key, t.Status)
		if now.Status != store.StatusTodo { // merged PR with --note, or a follow-up sub-task took the comment
			res = fmt.Sprintf("%s: delivered, task stays %s", t.Key, now.Status)
		}
	}
	store.RecordEvent(s.DB, f.ID, "message", "to "+t.Key+": "+firstLine(body))
	store.RecordEvent(s.DB, t.ID, "message", "from "+f.Key+": "+firstLine(body))
	return res, nil
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if len(l) > 120 {
		l = l[:120] + "…"
	}
	return l
}
