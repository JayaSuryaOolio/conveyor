package core

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/statusblock"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// A loop coordinates two tasks: a worker produces (usually a PR) and a checker judges it.
// Turns pass on each finished run (ProcessStop -> LoopOnStop), never by polling:
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
	if w.Status == store.StatusReview {
		return l, workerDone(s, &l, w)
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

// LoopOnStop passes the turn after an auto run of task finished (internal/hook ProcessStop).
func LoopOnStop(db *sql.DB, task store.Task, block statusblock.Block, hasBlock bool, reply string) error {
	if !hasBlock || block.Status != "done" {
		return nil // failed runs retry, blockless ones wait on the human: the turn stays
	}
	loops, err := store.TaskLoops(db, task.ID, true)
	if err != nil {
		return err
	}
	if len(loops) == 0 {
		return nil
	}
	if task, err = store.GetTask(db, task.ID); err != nil { // the status ProcessStop just set
		return err
	}
	s := &Service{DB: db}
	var errs []error
	for _, l := range loops {
		switch {
		case l.Turn == store.TurnWorker && l.WorkerID == task.ID:
			if task.Status != store.StatusReview {
				continue // queued comments or an epic gate: the worker is not finished yet
			}
			errs = append(errs, workerDone(s, &l, task))
		case l.Turn == store.TurnChecker && l.CheckerID == task.ID:
			errs = append(errs, checkerDone(s, &l, block.Verdict, reply))
		}
	}
	return errors.Join(errs...)
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
func workerDone(s *Service, l *store.Loop, w store.Task) error {
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
	l.Round, l.LastSHA, l.Turn = l.Round+1, sha, store.TurnChecker
	if err := store.SaveLoop(s.DB, *l); err != nil {
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
	_, err = s.AddComment(l.Checker, store.AuthorSystem, body, Ask)
	return err
}

// checkerDone ends the loop on approval or forwards the checker's reply to the worker.
func checkerDone(s *Service, l *store.Loop, verdict, reply string) error {
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
	l.Turn = store.TurnWorker
	if err := store.SaveLoop(s.DB, *l); err != nil {
		return err
	}
	next := "Fix them, push, and end with status done: the loop sends it back for another round."
	if l.Round >= l.MaxRounds {
		next = "This was the last round: fix them, push, and a human reviews the rest."
	}
	body := fmt.Sprintf("%s loop, round %d/%d: %s asks for changes.\n\n%s\n\n%s", l.Kind, l.Round, l.MaxRounds, l.Checker, strings.TrimSpace(reply), next)
	_, err = s.AddComment(l.Worker, store.AuthorSystem, body, FollowUp)
	return err
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
