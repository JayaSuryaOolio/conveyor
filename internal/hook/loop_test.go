package hook

import (
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// loopEnv is a worker task (e.task) and a checker task in an active loop.
type loopEnv struct {
	env
	checker store.Task
	svc     *core.Service
}

func newLoop(t *testing.T, kind string, rounds int) loopEnv {
	t.Helper()
	e := setup(t, store.StatusRunning)
	c, err := store.CreateTask(e.db, store.Task{Title: "reviewer", Status: store.StatusReview}, []store.TaskRepo{{RepoID: e.task.Repos[0].RepoID, IsPrimary: true, Branch: "r"}})
	if err != nil {
		t.Fatal(err)
	}
	le := loopEnv{env: e, checker: c, svc: &core.Service{DB: e.db}}
	if _, err := le.svc.StartLoop(e.task.Key, c.Key, kind, "", rounds); err != nil {
		t.Fatal(err)
	}
	return le
}

// finish ends an auto run of task with msg, moving it to running first as a launch would.
func (le loopEnv) finish(t *testing.T, key, msg string) {
	t.Helper()
	tk, err := store.GetTaskByKey(le.db, key)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != store.StatusRunning {
		if tk.Status != store.StatusTodo {
			if _, err := store.UpdateTaskStatus(le.db, tk.ID, store.StatusTodo, store.ActorSystem); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.UpdateTaskStatus(le.db, tk.ID, store.StatusRunning, store.ActorSystem); err != nil {
			t.Fatal(err)
		}
		tk.Status = store.StatusRunning
	}
	r, err := store.CreateRun(le.db, store.Run{TaskID: tk.ID, Kind: "auto", Mode: "fresh", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	pending, _ := store.UnconsumedComments(le.db, tk.ID) // the launch delivered them
	var ids []int64
	for _, c := range pending {
		ids = append(ids, c.ID)
	}
	if err := store.ConsumeComments(le.db, ids, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := ProcessStop(le.db, tk, r, msg, ""); err != nil {
		t.Fatal(err)
	}
}

func (le loopEnv) head(t *testing.T, sha string) {
	t.Helper()
	if err := store.SavePRReview(le.db, store.PRReview{TaskID: le.task.ID, RepoID: le.task.Repos[0].RepoID, HeadSHA: sha}); err != nil {
		t.Fatal(err)
	}
}

func (le loopEnv) loop(t *testing.T) store.Loop {
	t.Helper()
	ls, err := store.TaskLoops(le.db, le.task.ID, false)
	if err != nil || len(ls) != 1 {
		t.Fatalf("loops %v %v", ls, err)
	}
	return ls[0]
}

func lastComment(t *testing.T, le loopEnv, taskID int64) string {
	t.Helper()
	cs, _ := store.ListComments(le.db, taskID)
	if len(cs) == 0 {
		return ""
	}
	return cs[len(cs)-1].Body
}

func verdictMsg(v string) string {
	return "Line 3 is wrong.\n<conveyor>\nstatus: done\nsummary: s\nverdict: " + v + "\n</conveyor>"
}

func TestLoopReviewThenApprove(t *testing.T) {
	le := newLoop(t, "peer-review", 3)
	le.head(t, "aaa")
	le.finish(t, le.task.Key, doneMsg)
	if l := le.loop(t); l.Round != 1 || l.Turn != store.TurnChecker || l.LastSHA != "aaa" {
		t.Fatalf("%+v", l)
	}
	if c, _ := store.GetTask(le.db, le.checker.ID); c.Status != store.StatusTodo || !strings.Contains(lastComment(t, le, c.ID), "verdict: approved") {
		t.Fatalf("checker not asked: %s %q", c.Status, lastComment(t, le, c.ID))
	}

	le.finish(t, le.checker.Key, verdictMsg("changes"))
	if w := le.status(t); w.Status != store.StatusTodo || !strings.Contains(lastComment(t, le, w.ID), "Line 3 is wrong.") {
		t.Fatalf("worker not sent findings: %s", w.Status)
	}
	if l := le.loop(t); l.Turn != store.TurnWorker {
		t.Fatalf("%+v", l)
	}

	le.head(t, "bbb")
	le.finish(t, le.task.Key, doneMsg)
	le.finish(t, le.checker.Key, verdictMsg("approved"))
	if l := le.loop(t); l.State != store.LoopApproved || l.Round != 2 {
		t.Fatalf("%+v", l)
	}
	if w := le.status(t); w.Status != store.StatusReview {
		t.Fatalf("approval must not requeue the worker: %s", w.Status)
	}
}

func TestLoopStops(t *testing.T) {
	cases := map[string]struct {
		rounds int
		steps  func(t *testing.T, le loopEnv)
		reason string
	}{
		"same head": {3, func(t *testing.T, le loopEnv) {
			le.finish(t, le.checker.Key, verdictMsg("changes"))
			le.finish(t, le.task.Key, doneMsg) // no push
		}, "no new commits"},
		"max rounds": {1, func(t *testing.T, le loopEnv) {
			le.finish(t, le.checker.Key, verdictMsg("changes"))
			le.head(t, "bbb")
			le.finish(t, le.task.Key, doneMsg)
		}, "max 1 rounds"},
		"no verdict": {3, func(t *testing.T, le loopEnv) {
			le.finish(t, le.checker.Key, doneMsg)
		}, "no verdict"},
		"human comment": {3, func(t *testing.T, le loopEnv) {
			if _, err := le.svc.AddComment(le.checker.Key, store.AuthorHuman, "hold on", core.FollowUp); err != nil {
				t.Fatal(err)
			}
			le.finish(t, le.checker.Key, verdictMsg("changes"))
		}, "human comment"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			le := newLoop(t, "spec-review", tc.rounds)
			le.head(t, "aaa")
			le.finish(t, le.task.Key, doneMsg)
			tc.steps(t, le)
			if l := le.loop(t); l.State != store.LoopStopped || !strings.Contains(l.Reason, tc.reason) {
				t.Fatalf("%+v", l)
			}
		})
	}
}

// A request to a checker that is mid-run is queued for its next run, not a second session.
func TestLoopCheckerMidRunQueued(t *testing.T) {
	le := newLoop(t, "peer-review", 3)
	store.UpdateTaskStatus(le.db, le.checker.ID, store.StatusTodo, store.ActorSystem)
	store.UpdateTaskStatus(le.db, le.checker.ID, store.StatusRunning, store.ActorSystem)
	le.finish(t, le.task.Key, doneMsg)
	if c, _ := store.GetTask(le.db, le.checker.ID); c.Status != store.StatusRunning {
		t.Fatalf("checker %s", c.Status)
	}
	if p, _ := store.UnconsumedComments(le.db, le.checker.ID); len(p) != 1 {
		t.Fatalf("want 1 queued comment, got %d", len(p))
	}
}

func TestLoopStartGuards(t *testing.T) {
	le := newLoop(t, "peer-review", 3)
	if _, err := le.svc.StartLoop(le.task.Key, le.checker.Key, "peer-review", "", 3); err == nil {
		t.Fatal("second active loop accepted")
	}
	if _, err := le.svc.StartLoop(le.task.Key, le.task.Key, "peer-review", "", 3); err == nil {
		t.Fatal("self loop accepted")
	}
	if _, err := le.svc.StartLoop(le.task.Key, le.checker.Key, "custom", "", 3); err == nil {
		t.Fatal("custom without prompt accepted")
	}
	rel, err := store.ListRelations(le.db, le.status(t))
	if err != nil || len(rel.Links) != 1 || rel.Links[0].Kind != store.LinkLoop || !rel.Links[0].From {
		t.Fatalf("%+v %v", rel.Links, err)
	}
	if err := le.svc.Unlink(le.task.Key, store.LinkLoop, le.checker.Key); err != nil {
		t.Fatal(err)
	}
	if ls, _ := store.TaskLoops(le.db, le.task.ID, false); len(ls) != 0 {
		t.Fatalf("unlink left %v", ls)
	}
}
