package gh

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// fakeAPI installs a gh that serves $FAKE/{reviews,comments,threads}.json for `gh api` calls.
func fakeAPI(t *testing.T, reviews, comments, threads string) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$2" in
*/reviews*) cat "$FAKE/reviews.json";;
*/issues/*/comments*) cat "$FAKE/convo.json";;
*/comments*) cat "$FAKE/comments.json";;
graphql) cat "$FAKE/threads.json";;
*/check-runs*) cat "$FAKE/runs.json";;
*/pulls/*) cat "$FAKE/pulls.json";;
esac
`
	os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755)
	for n, v := range map[string]string{"reviews": reviews, "comments": comments, "threads": threads, "convo": `[]`, "runs": `{"check_runs":[]}`, "pulls": `{}`} {
		os.WriteFile(filepath.Join(dir, n+".json"), []byte(v), 0o644)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("FAKE", dir)
	return dir
}

func reviewTask(t *testing.T) (*core.Service, store.Task) {
	t.Helper()
	db, tk := newTask(t, 1)
	url, open := "https://github.com/o/r/pull/7", "OPEN"
	tr := tk.Repos[0]
	tr.PRURL, tr.PRState = &url, &open
	if err := store.UpdateTaskRepo(db, tr); err != nil {
		t.Fatal(err)
	}
	return &core.Service{DB: db}, tk
}

const threads = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
 {"isResolved":true,"comments":{"nodes":[{"databaseId":12}]}},
 {"isResolved":false,"comments":{"nodes":[{"databaseId":11}]}}]}}}}}`

func TestReviewCheckPostsOnceThenBacksOff(t *testing.T) {
	dir := fakeAPI(t,
		`[{"id":10,"body":"please rename","state":"CHANGES_REQUESTED","user":{"login":"bob"}}]`,
		`[{"id":11,"body":"nil deref","path":"a.go","line":3,"user":{"login":"bob"}},
		  {"id":12,"body":"resolved already","path":"a.go","line":9,"user":{"login":"bob"}},
		  {"id":13,"body":"ai agent: fixed","path":"a.go","line":3,"user":{"login":"me"}}]`,
		threads)
	os.WriteFile(filepath.Join(dir, "runs.json"), []byte(`{"check_runs":[{"name":"test","conclusion":"failure"},{"name":"lint","conclusion":"success"}]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "pulls.json"), []byte(`{"mergeable_state":"dirty","head":{"sha":"abc"}}`), 0o644)
	os.WriteFile(filepath.Join(dir, "convo.json"), []byte(`[{"id":14,"body":"human says hi","user":{"login":"carol"}}]`), 0o644)
	svc, tk := reviewTask(t)
	ctx := context.Background()

	if _, err := ReviewCheck(ctx, svc, tk.Key, 0); err != nil {
		t.Fatal(err)
	}
	cs, _ := store.ListComments(svc.DB, tk.ID)
	if len(cs) != 1 || cs[0].Author != store.AuthorSystem {
		t.Fatalf("want one system comment, got %+v", cs)
	}
	for _, want := range []string{"PR review on https://github.com/o/r/pull/7:", "> a.go:3 (bob)\nnil deref", "please rename", "> comment (carol)\nhuman says hi", "Failing CI checks: test.", "merge conflicts"} {
		if !strings.Contains(cs[0].Body, want) {
			t.Errorf("missing %q in %q", want, cs[0].Body)
		}
	}
	for _, no := range []string{"resolved already", "ai agent: fixed"} {
		if strings.Contains(cs[0].Body, no) {
			t.Errorf("%q leaked into %q", no, cs[0].Body)
		}
	}
	if got, _ := store.GetTask(svc.DB, tk.ID); got.Status != store.StatusTodo {
		t.Errorf("task not requeued: %s", got.Status)
	}
	st, _ := store.GetPRReview(svc.DB, tk.ID, tk.Repos[0].RepoID)
	if st.SeenReviewID != 10 || st.SeenInlineID != 13 || st.SeenCommentID != 14 || st.Unresolved != 1 || st.Review != "CHANGES_REQUESTED" {
		t.Errorf("state %+v", st)
	}

	// Second check: same CI/conflict state, so nothing new, no comment, next backoff step (30m).
	if _, err := ReviewCheck(ctx, svc, tk.Key, 0); err != nil {
		t.Fatal(err)
	}
	if cs, _ = store.ListComments(svc.DB, tk.ID); len(cs) != 1 {
		t.Fatalf("second check commented: %d comments", len(cs))
	}
	a, err := store.GetAutomation(svc.DB, "track-pr:"+tk.Key)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(mustTime(t, *a.RunAt)); d < 29*time.Minute || d > 31*time.Minute {
		t.Errorf("next check in %v", d)
	}
	if !strings.Contains(a.ActionArg, `"--step","1"`) {
		t.Errorf("arg %s", a.ActionArg)
	}
}

// CV-195: GitHub issue comment ids run above inline comment and review ids, so a newer issue
// comment must not hide inline comments and reviews posted after it.
func TestReviewCheckWatermarkPerType(t *testing.T) {
	noThreads := `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]}}}}}`
	dir := fakeAPI(t, `[]`, `[]`, noThreads)
	os.WriteFile(filepath.Join(dir, "convo.json"), []byte(`[{"id":6954124677,"body":"Changeset detected","user":{"login":"changeset-bot"}}]`), 0o644)
	svc, tk := reviewTask(t)
	ctx := context.Background()
	if _, err := ReviewCheck(ctx, svc, tk.Key, 0); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "reviews.json"), []byte(`[{"id":5950000000,"body":"looks off","state":"COMMENTED","user":{"login":"bob"}}]`), 0o644)
	os.WriteFile(filepath.Join(dir, "comments.json"), []byte(`[{"id":4610000000,"body":"handle nil","path":"update_overrides.go","line":5,"user":{"login":"bob"}}]`), 0o644)
	if _, err := ReviewCheck(ctx, svc, tk.Key, 0); err != nil {
		t.Fatal(err)
	}
	cs, _ := store.ListComments(svc.DB, tk.ID)
	if len(cs) != 1 || !strings.Contains(cs[0].Body, "handle nil") || !strings.Contains(cs[0].Body, "looks off") {
		t.Fatalf("missed inline comment or review: %+v", cs)
	}
}

// A row migrated from the single watermark (-1 marks) baselines reviews and inline comments
// on its next check instead of resending feedback the agent already handled.
func TestReviewCheckBaselinesMigratedRow(t *testing.T) {
	dir := fakeAPI(t, `[{"id":10,"body":"old review","state":"COMMENTED","user":{"login":"bob"}}]`,
		`[{"id":11,"body":"old inline","path":"a.go","line":1,"user":{"login":"bob"}}]`,
		`{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]}}}}}`)
	svc, tk := reviewTask(t)
	rid := tk.Repos[0].RepoID
	store.SavePRReview(svc.DB, store.PRReview{TaskID: tk.ID, RepoID: rid, SeenReviewID: -1, SeenInlineID: -1, SeenCommentID: 99})
	ctx := context.Background()
	if _, err := ReviewCheck(ctx, svc, tk.Key, 0); err != nil {
		t.Fatal(err)
	}
	if cs, _ := store.ListComments(svc.DB, tk.ID); len(cs) != 0 {
		t.Fatalf("resent old feedback: %+v", cs)
	}
	if st, _ := store.GetPRReview(svc.DB, tk.ID, rid); st.SeenReviewID != 10 || st.SeenInlineID != 11 || st.SeenCommentID != 99 {
		t.Fatalf("state %+v", st)
	}
	os.WriteFile(filepath.Join(dir, "comments.json"), []byte(`[{"id":11,"body":"old inline","path":"a.go","line":1,"user":{"login":"bob"}},{"id":12,"body":"new inline","path":"a.go","line":2,"user":{"login":"bob"}}]`), 0o644)
	if _, err := ReviewCheck(ctx, svc, tk.Key, 0); err != nil {
		t.Fatal(err)
	}
	if cs, _ := store.ListComments(svc.DB, tk.ID); len(cs) != 1 || !strings.Contains(cs[0].Body, "new inline") || strings.Contains(cs[0].Body, "old inline") {
		t.Fatalf("want only the new inline comment: %+v", cs)
	}
}

func TestBackoffSchedule(t *testing.T) {
	fakeAPI(t, `[]`, `[]`, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]}}}}}`)
	svc, tk := reviewTask(t)
	for step, want := range backoff {
		if _, err := ReviewCheck(context.Background(), svc, tk.Key, step); err != nil {
			t.Fatal(err)
		}
		a, err := store.GetAutomation(svc.DB, "track-pr:"+tk.Key)
		if err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		if d := time.Until(mustTime(t, *a.RunAt)); d < want-time.Minute || d > want+time.Minute {
			t.Errorf("step %d: next in %v, want %v", step, d, want)
		}
	}
	store.DeleteAutomation(svc.DB, "track-pr:"+tk.Key)
	if _, err := ReviewCheck(context.Background(), svc, tk.Key, len(backoff)); err != nil {
		t.Fatal(err)
	}
	if as, _ := store.ListAutomations(svc.DB); len(as) != 0 {
		t.Fatalf("backoff did not stop: %+v", as)
	}
}

func TestNewHeadSchedulesCheckIn10Min(t *testing.T) {
	dir := fakeGH(t)
	db, tk := newTask(t, 1)
	setPRHead(t, dir, tk.Repos[0].Branch, "aaa")
	sync(t, db, tk)
	a, err := store.GetAutomation(db, "track-pr:"+tk.Key)
	if err != nil {
		t.Fatal(err)
	}
	first := *a.RunAt
	sync(t, db, tk) // same head: untouched
	if a, _ = store.GetAutomation(db, "track-pr:"+tk.Key); *a.RunAt != first {
		t.Error("rescheduled without a push")
	}
	if d := time.Until(mustTime(t, first)); d < 9*time.Minute || d > 11*time.Minute {
		t.Errorf("first check in %v", d)
	}
	setPRHead(t, dir, tk.Repos[0].Branch, "bbb") // push replaces the pending check, not adds one
	sync(t, db, tk)
	if as, _ := store.ListAutomations(db); len(as) != 1 {
		t.Fatalf("%d automations", len(as))
	}
}

func setPRHead(t *testing.T, dir, branch, sha string) {
	t.Helper()
	body := `[{"url":"https://github.com/o/r/pull/7","state":"OPEN","number":7,"headRefOid":"` + sha + `"}]`
	os.WriteFile(filepath.Join(dir, strings.ReplaceAll(branch, "/", "_")+".json"), []byte(body), 0o644)
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCleanStripsBotBoilerplate(t *testing.T) {
	in := "<!-- cubic:review-summary:start -->\n**8 issues found**\n<!-- end -->\n\n---\n- [ ] autofix\n<details>\n<summary>Prompt for AI agents</summary>\nfix everything\n</details>"
	if got := clean(in); got != "**8 issues found**" {
		t.Errorf("got %q", got)
	}
	if got := clean(strings.Repeat("x", 5000)); len(got) > itemCap+4 {
		t.Errorf("not capped: %d", len(got))
	}
}

func TestIsNoise(t *testing.T) {
	if !isNoise("### ⚠️ No Changeset found\n\nLatest commit") || !isNoise("## Draft PR not reviewed") || isNoise("real finding") {
		t.Fatal("isNoise")
	}
}

// Feedback dropped for lack of room keeps the watermark behind it, so the next check reports it.
func TestReviewCheckDeferredItemNotLost(t *testing.T) {
	noThreads := `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]}}}}}`
	big := strings.Repeat("x", itemCap-10)
	var cs []string
	for i := 1; i <= 7; i++ { // 7 x ~1.5KB overflows totalCap (8000)
		cs = append(cs, `{"id":`+strconv.Itoa(i)+`,"body":"`+big+`","path":"a.go","line":1,"user":{"login":"bob"}}`)
	}
	dir := fakeAPI(t, `[]`, `[`+strings.Join(cs, ",")+`]`, noThreads)
	svc, tk := reviewTask(t)
	ctx := context.Background()
	st := store.PRReview{TaskID: tk.ID, RepoID: tk.Repos[0].RepoID, SeenReviewID: 0, SeenInlineID: 0, SeenCommentID: 0}
	if err := store.SavePRReview(svc.DB, st); err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewCheck(ctx, svc, tk.Key, 0); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetPRReview(svc.DB, tk.ID, tk.Repos[0].RepoID)
	if got.SeenInlineID >= 7 || got.SeenInlineID == 0 {
		t.Fatalf("watermark should stop before the dropped items: %+v", got)
	}
	// The next check reports the rest.
	os.WriteFile(filepath.Join(dir, "comments.json"), []byte(`[`+strings.Join(cs, ",")+`]`), 0o644)
	if _, err := ReviewCheck(ctx, svc, tk.Key, 0); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetPRReview(svc.DB, tk.ID, tk.Repos[0].RepoID)
	if got.SeenInlineID != 7 {
		t.Fatalf("deferred items never reported: %+v", got)
	}
}

// A gh failure reschedules the same step instead of ending the backoff chain.
func TestReviewCheckReschedulesAfterError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\necho 'error connecting to api.github.com' >&2\nexit 1\n"), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	svc, tk := reviewTask(t)
	if _, err := ReviewCheck(context.Background(), svc, tk.Key, 1); err == nil {
		t.Fatal("want error")
	}
	a, err := store.GetAutomation(svc.DB, "track-pr:"+tk.Key)
	if err != nil {
		t.Fatalf("not rescheduled: %v", err)
	}
	if !strings.Contains(a.ActionArg, `"--step","1"`) {
		t.Errorf("arg %s", a.ActionArg)
	}
}

func TestReviewHeaderByReviewerKind(t *testing.T) {
	noThreads := `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]}}}}}`
	for _, c := range []struct {
		name, review       string
		wantHuman, wantBot bool
	}{
		{"human", `{"login":"bob"}`, true, false},
		{"bot", `{"login":"coderabbitai[bot]","type":"Bot"}`, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			fakeAPI(t, `[{"id":10,"body":"please rename","state":"COMMENTED","user":`+c.review+`}]`, `[]`, noThreads)
			svc, tk := reviewTask(t)
			if _, err := ReviewCheck(context.Background(), svc, tk.Key, 0); err != nil {
				t.Fatal(err)
			}
			cs, _ := store.ListComments(svc.DB, tk.ID)
			if len(cs) != 1 {
				t.Fatalf("want one comment, got %d", len(cs))
			}
			b := cs[0].Body
			if got := strings.Contains(b, "do not resolve"); got != c.wantHuman {
				t.Errorf("human rule present=%v in %q", got, b)
			}
			if got := strings.Contains(b, "then resolve it"); got != c.wantBot {
				t.Errorf("bot rule present=%v in %q", got, b)
			}
			if !strings.Contains(b, "Decision needed:") {
				t.Errorf("no decision rule in %q", b)
			}
		})
	}
}

func TestReviewCheckStoresChecksOnEpicFlow(t *testing.T) {
	dir := fakeAPI(t, `[]`, `[]`, threads)
	os.WriteFile(filepath.Join(dir, "runs.json"), []byte(`{"check_runs":[{"name":"test","conclusion":"failure"},{"name":"lint","conclusion":"success"}]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "pulls.json"), []byte(`{"mergeable_state":"clean","head":{"sha":"abc"}}`), 0o644)
	svc, tk := reviewTask(t)
	if err := store.MakeEpic(svc.DB, tk.ID, "e"); err != nil {
		t.Fatal(err)
	}
	f, err := store.StartFlow(svc.DB, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	tk, _ = store.GetTask(svc.DB, tk.ID)
	if err := store.SyncFlowTasks(svc.DB, f.ID, []store.Task{tk}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewCheck(context.Background(), svc, tk.Key, 0); err != nil {
		t.Fatal(err)
	}
	cs, _ := store.ListComments(svc.DB, tk.ID)
	if len(cs) != 1 || !strings.Contains(cs[0].Body, "Failing CI checks: test.") {
		t.Fatalf("want one CI comment, got %+v", cs)
	}
	prs, _ := store.ListFlowPRs(svc.DB, f.ID)
	if len(prs) != 1 || prs[0].Checks == nil || *prs[0].Checks != `["test"]` || *prs[0].HeadSHA != "abc" {
		t.Errorf("flow pr %+v", prs)
	}
}
