package core

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// A loop coordinates two tasks: a worker produces (usually a PR) and a checker judges it.
// The hook records each finished run (status block, verdict); the dispatcher tick (TickLoops) passes the turn,
// and the turn is a message through the receiving task's comment inbox (Message), so it shows in its thread:
//   - worker ends a run with status done -> round+1, the checker is asked (system comment, requeues it)
//   - checker ends with `verdict: approved` -> loop approved; `verdict: changes` -> its reply goes to the worker
//
// Guards: MaxRounds requests to the checker; the worker's PR head must move between rounds;
// a checker reply without a verdict, a merged worker PR or a human comment on either task stops it.

// LoopKinds are the loop flavours: what the checker is asked to do each round.
// %[1]s is the worker key, %[2]s its PR (or "its work" without one).
var LoopKinds = map[string]string{
	"peer-review": "Review %[2]s of %[1]s as a peer: correctness bugs, missed edge cases, tests, and this repo's conventions (its CLAUDE.md). Read the diff, do not change it.",
	"spec-review": "Check %[2]s of %[1]s against its description and acceptance criteria (`conveyor task show %[1]s`): anything missing, wrong, or out of scope.",
	"custom":      "Check %[2]s of %[1]s as instructed below.",
}

// DefaultLoopRounds bounds a loop started without --max-rounds.
const DefaultLoopRounds = 3

// StartLoop starts a loop where checker checks worker. If the worker is already in review,
// the checker is asked right away; otherwise on the worker's next finished run.
func (s *Service) StartLoop(worker, checker, kind, prompt string, maxRounds int) (store.Loop, error) {
	if _, ok := LoopKinds[kind]; !ok {
		return store.Loop{}, fmt.Errorf("unknown loop kind %q (want one of %s)", kind, strings.Join(slices.Sorted(maps.Keys(LoopKinds)), ", "))
	}
	if kind == "custom" && strings.TrimSpace(prompt) == "" {
		return store.Loop{}, fmt.Errorf("a custom loop needs --prompt")
	}
	if maxRounds <= 0 {
		maxRounds = DefaultLoopRounds
	}
	w, err := store.GetTaskByKey(s.DB, worker)
	if err != nil {
		return store.Loop{}, err
	}
	c, err := store.GetTaskByKey(s.DB, checker)
	if err != nil {
		return store.Loop{}, err
	}
	if w.ID == c.ID {
		return store.Loop{}, fmt.Errorf("a task cannot check itself")
	}
	for _, t := range []store.Task{w, c} { // a loop talks through the inbox, and nothing starts these
		if t.Status == store.StatusBacklog || t.Status == store.StatusCancelled {
			return store.Loop{}, fmt.Errorf("%s is %s: a human moves it to todo first", t.Key, t.Status)
		}
	}
	for _, id := range []int64{w.ID, c.ID} { // one active loop per task keeps turns unambiguous
		if act, err := store.TaskLoops(s.DB, id, true); err != nil {
			return store.Loop{}, err
		} else if len(act) > 0 {
			return store.Loop{}, fmt.Errorf("%s and %s are already in an active loop (remove it with `conveyor task unlink %s loop %s`)", act[0].Worker, act[0].Checker, act[0].Worker, act[0].Checker)
		}
	}
	l, err := store.CreateLoop(s.DB, store.Loop{WorkerID: w.ID, CheckerID: c.ID, Worker: w.Key, Checker: c.Key, Kind: kind, Prompt: strings.TrimSpace(prompt),
		MaxRounds: maxRounds, Turn: store.TurnWorker, State: store.LoopActive})
	if err != nil {
		return l, err
	}
	store.RecordEvent(s.DB, w.ID, "loop", fmt.Sprintf("started %s loop with checker %s (max %d rounds)", kind, c.Key, maxRounds))
	// The next tick asks the checker at once if the worker already waits in review; else after its next finished run.
	if w.Status != store.StatusReview {
		if r, err := store.LatestRun(s.DB, w.ID); err == nil {
			l.TurnRun = r.ID
			err = store.SaveLoop(s.DB, l)
			return l, err
		}
	}
	return l, nil
}

// RemoveLoops deletes the loops between worker and checker (`task unlink W loop C`).
func (s *Service) RemoveLoops(worker, checker string) error {
	a, b, err := s.pair(worker, checker)
	if err != nil {
		return err
	}
	n, err := store.DeleteLoops(s.DB, a, b)
	if err == nil && n == 0 {
		err = fmt.Errorf("no loop with worker %s and checker %s", worker, checker)
	}
	return err
}

// TickLoops reconciles every active loop with the runs it has seen (dispatcher tick, like wakeEpics).
// The hook only records facts on the run (status block, verdict); this passes the turn from them.
// It is idempotent: a turn passes once, because the loop saves a watermark (TurnRun) before messaging
// and the next holder must finish a run newer than it.
func (s *Service) TickLoops() error {
	loops, err := store.ListLoops(s.DB)
	if err != nil {
		return err
	}
	var errs []error
	for _, l := range loops {
		if l.State == store.LoopActive {
			errs = append(errs, s.tickLoop(&l))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) tickLoop(l *store.Loop) error {
	holderID := l.WorkerID
	if l.Turn == store.TurnChecker {
		holderID = l.CheckerID
	}
	h, err := store.GetTask(s.DB, holderID)
	if err != nil {
		return err
	}
	r, err := store.LatestRun(s.DB, holderID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	// Not finished yet: still running, queued comments (todo), a failure that retries, an epic gate or a blockless run.
	if r.ID <= l.TurnRun || r.Kind != "auto" || r.Status != "finished" || h.Status != store.StatusReview ||
		r.BlockStatus == nil || *r.BlockStatus != "done" {
		return nil
	}
	if l.Turn == store.TurnWorker {
		return s.workerDone(l, h)
	}
	reply := ""
	if r.FinalMessage != nil {
		reply = *r.FinalMessage
	}
	return s.checkerDone(l, r.Verdict, reply)
}

// StopLoopsOnHuman stops task's active loops: a human stepped in.
func StopLoopsOnHuman(db *sql.DB, task store.Task) error {
	loops, err := store.TaskLoops(db, task.ID, true)
	if err != nil {
		return err
	}
	var errs []error
	for _, l := range loops {
		errs = append(errs, stopLoop(db, &l, "human comment on "+task.Key))
	}
	return errors.Join(errs...)
}

// workerDone asks the checker for the next round, unless a guard stops the loop.
func (s *Service) workerDone(l *store.Loop, w store.Task) error {
	if pr := mergedPR(w); pr != "" {
		return stopLoop(s.DB, l, "worker PR merged")
	}
	if l.Round >= l.MaxRounds {
		return stopLoop(s.DB, l, fmt.Sprintf("max %d rounds reached", l.MaxRounds))
	}
	pr, sha, err := primaryHead(s.DB, w)
	if err != nil {
		return err
	}
	if sha != "" && sha == l.LastSHA {
		return stopLoop(s.DB, l, "no new commits since round "+fmt.Sprint(l.Round))
	}
	l.Round, l.LastSHA = l.Round+1, sha
	if err := s.passTurn(l, store.TurnChecker, l.CheckerID); err != nil {
		return err
	}
	what := "the work"
	if pr != "" {
		what = "PR " + pr
		if sha != "" {
			what += " (head " + shortSHA(sha) + ")"
		}
	}
	body := fmt.Sprintf("%s loop, round %d/%d. %s", l.Kind, l.Round, l.MaxRounds, fmt.Sprintf(LoopKinds[l.Kind], l.Worker, what))
	if l.Prompt != "" {
		body += "\n\n" + l.Prompt
	}
	body += fmt.Sprintf("\n\nPut your findings in your final reply: Conveyor forwards it to %s, so do not comment on that task yourself. "+
		"Add `verdict: approved` (nothing left to fix) or `verdict: changes` (list what to fix) to your <conveyor> block. A missing verdict stops the loop.", l.Worker)
	return s.send(l, l.Worker, l.Checker, body, Ask)
}

// checkerDone ends the loop on approval or forwards the checker's reply to the worker.
func (s *Service) checkerDone(l *store.Loop, verdict, reply string) error {
	switch verdict {
	case "approved":
		l.State, l.Reason = store.LoopApproved, ""
		if err := store.SaveLoop(s.DB, *l); err != nil {
			return err
		}
		return note(s.DB, l.WorkerID, fmt.Sprintf("%s loop: %s approved in round %d/%d.", l.Kind, l.Checker, l.Round, l.MaxRounds))
	case "changes":
	default:
		return stopLoop(s.DB, l, "checker gave no verdict")
	}
	w, err := store.GetTask(s.DB, l.WorkerID)
	if err != nil {
		return err
	}
	if mergedPR(w) != "" {
		return stopLoop(s.DB, l, "worker PR merged")
	}
	if err := s.passTurn(l, store.TurnWorker, l.WorkerID); err != nil {
		return err
	}
	next := "Fix them, push, and end with status done: the loop sends it back for another round."
	if l.Round >= l.MaxRounds {
		next = "This was the last round: fix them, push, and a human reviews the rest."
	}
	body := fmt.Sprintf("%s loop, round %d/%d: %s asks for changes.\n\n%s\n\n%s", l.Kind, l.Round, l.MaxRounds, l.Checker, strings.TrimSpace(reply), next)
	return s.send(l, l.Checker, l.Worker, body, FollowUp)
}

// passTurn saves the loop with the turn handed to taskID, before the message that asks it to act
// (a crash between the two loses one message and the loop waits for a human; the reverse would repeat it).
// The new holder's finished runs so far do not count: it must finish a newer one.
func (s *Service) passTurn(l *store.Loop, turn string, taskID int64) error {
	l.Turn, l.TurnRun = turn, 0
	if r, err := store.LatestRun(s.DB, taskID); err == nil {
		l.TurnRun = r.ID
	}
	return store.SaveLoop(s.DB, *l)
}

// send delivers a loop message task to task through the regular inbox; a target that cannot take it stops the loop.
func (s *Service) send(l *store.Loop, from, to, body string, after AfterMerge) error {
	if _, err := s.Message(from, to, body, after); err != nil {
		return stopLoop(s.DB, l, "cannot message "+to+": "+err.Error())
	}
	return nil
}

// stopLoop marks l stopped and tells the worker's thread why, without requeueing anything.
func stopLoop(db *sql.DB, l *store.Loop, reason string) error {
	l.State, l.Reason = store.LoopStopped, reason
	if err := store.SaveLoop(db, *l); err != nil {
		return err
	}
	return note(db, l.WorkerID, fmt.Sprintf("%s loop with %s stopped after round %d/%d: %s.", l.Kind, l.Checker, l.Round, l.MaxRounds, reason))
}

// note adds a system comment that does not requeue the task.
func note(db *sql.DB, taskID int64, body string) error {
	_, err := store.AddComment(db, store.Comment{TaskID: taskID, Author: store.AuthorSystem, Body: body})
	if err == nil {
		err = store.RecordEvent(db, taskID, "loop", body)
	}
	return err
}

// primaryHead is the worker's primary-repo PR URL and its last synced head sha ("" when unknown).
func primaryHead(db *sql.DB, t store.Task) (pr, sha string, err error) {
	for _, r := range t.Repos {
		if !r.IsPrimary {
			continue
		}
		if r.PRURL != nil {
			pr = *r.PRURL
		}
		st, err := store.GetPRReview(db, t.ID, r.RepoID)
		return pr, st.HeadSHA, err
	}
	return "", "", nil
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
