package core

import (
	"fmt"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// SourceTask is the comment source of a message from another task: "task:CV-5". The app shows it as
// "from CV-5", so a reader can tell a task's message from the app's own.
const SourceTask = "task:"

// MaxReplyRequests caps the --reply messages between two tasks (either direction) since the last human
// comment on either, so two agents cannot keep each other busy without a human seeing it.
const MaxReplyRequests = 3

// Message sends body from task `from` to task `to`. It is a comment in the target's inbox (the same
// queue humans use: delivered by the next run, `consumed_by_run_id`), authored system with the sender
// as source, so it never counts as a human stepping in.
//
// With wantsReply, Conveyor guarantees an answer: when the run that reads it ends, TickReplies sends
// that run's final message (or why it stopped) back to `from`.
//
// The result says what it did, so a sender is never left guessing:
//   - target running or todo: queued, read by its next run
//   - target in review, needs_you, failed or done: requeued
//   - target backlog or cancelled: refused (only a human starts those), nothing is stored
func (s *Service) Message(from, to, body string, after AfterMerge, wantsReply bool) (string, error) {
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
	if wantsReply {
		n, err := store.ReplyRequestsSinceHuman(s.DB, f, t)
		if err != nil {
			return "", err
		}
		if n >= MaxReplyRequests {
			return "", fmt.Errorf("%s and %s already exchanged %d --reply messages since the last human comment on either: a human comments to allow more", f.Key, t.Key, n)
		}
	}
	c, err := s.AddCommentVia(t.Key, store.AuthorSystem, SourceTask+f.Key, body, after)
	if err != nil {
		return "", err
	}
	if wantsReply {
		if err := store.SetWantsReply(s.DB, c.ID); err != nil {
			return "", err
		}
	}
	now, _ := store.GetTask(s.DB, t.ID) // the status the comment left it in
	res := fmt.Sprintf("%s: queued, it reads it with its next run", t.Key)
	if t.Status != store.StatusRunning && t.Status != store.StatusTodo {
		res = fmt.Sprintf("%s: requeued from %s, starts on the next dispatcher tick", t.Key, t.Status)
		if now.Status != store.StatusTodo { // merged PR with --note, or a follow-up sub-task took the comment
			res = fmt.Sprintf("%s: delivered, task stays %s", t.Key, now.Status)
		}
	}
	if wantsReply {
		res += "; its reply comes back to " + f.Key + " when that run ends"
	}
	store.RecordEvent(s.DB, f.ID, "message", "to "+t.Key+": "+firstLine(body))
	store.RecordEvent(s.DB, t.ID, "message", "from "+f.Key+": "+firstLine(body))
	return res, nil
}

// TickReplies sends the reply for every --reply message whose reading run has ended: the run's final
// message when it ended done, else "KEY stopped: <status> – <reason>", so the sender is never left
// waiting. The comment is marked replied before sending: a crash loses at most one reply, never
// duplicates one. The reply never asks for a reply itself. Called from the dispatcher tick.
func (s *Service) TickReplies() error {
	due, err := store.RepliesDue(s.DB)
	if err != nil {
		return err
	}
	var errs []string
	for _, c := range due {
		sender, ok := strings.CutPrefix(c.Source, SourceTask)
		if !ok {
			continue
		}
		if claimed, err := store.ClaimReply(s.DB, c.ID); err != nil || !claimed {
			if err != nil {
				errs = append(errs, err.Error())
			}
			continue
		}
		t, err := store.GetTask(s.DB, c.TaskID)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		r, err := store.GetRun(s.DB, *c.ConsumedByRunID)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if _, err := s.Message(t.Key, sender, replyBody(t, r), Ask, false); err != nil {
			errs = append(errs, fmt.Sprintf("reply %s -> %s: %v", t.Key, sender, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// replyBody is what run r of task t answers: its final message when it ended done, else why it stopped.
func replyBody(t store.Task, r store.Run) string {
	if r.Status != "failed" && r.BlockStatus != nil && *r.BlockStatus == "done" && t.Status != store.StatusCancelled {
		for _, m := range []*string{r.FinalMessage, r.Summary} {
			if m != nil && strings.TrimSpace(*m) != "" {
				return "reply from " + t.Key + ":\n\n" + strings.TrimSpace(*m)
			}
		}
		return "reply from " + t.Key + ": ended done without a final message"
	}
	status := string(t.Status)
	if r.Status == "failed" {
		status = "failed"
	} else if r.BlockStatus != nil && *r.BlockStatus != "done" && t.Status != store.StatusCancelled {
		status = *r.BlockStatus
	}
	reason := "no reason recorded"
	for _, m := range []*string{r.Error, r.Summary, r.FinalMessage} {
		if m != nil && strings.TrimSpace(*m) != "" {
			reason = firstLine(*m)
			break
		}
	}
	return fmt.Sprintf("%s stopped: %s – %s", t.Key, status, reason)
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if len(l) > 120 {
		l = l[:120] + "…"
	}
	return l
}
