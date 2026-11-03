package gh

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// fakeAPI installs a gh that serves $FAKE/{reviews,comments,threads}.json for `gh api` calls.
func fakeAPI(t *testing.T, reviews, comments, threads string) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$2" in
*/reviews*) cat "$FAKE/reviews.json";;
*/comments*) cat "$FAKE/comments.json";;
graphql) cat "$FAKE/threads.json";;
esac
`
	os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755)
	for n, v := range map[string]string{"reviews": reviews, "comments": comments, "threads": threads} {
		os.WriteFile(filepath.Join(dir, n+".json"), []byte(v), 0o644)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("FAKE", dir)
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
	fakeAPI(t,
		`[{"id":10,"body":"please rename","state":"CHANGES_REQUESTED","user":{"login":"bob"}}]`,
		`[{"id":11,"body":"nil deref","path":"a.go","line":3,"user":{"login":"bob"}},
		  {"id":12,"body":"resolved already","path":"a.go","line":9,"user":{"login":"bob"}},
		  {"id":13,"body":"ai agent: fixed","path":"a.go","line":3,"user":{"login":"me"}}]`,
		threads)
	svc, tk := reviewTask(t)
	ctx := context.Background()

	if _, err := ReviewCheck(ctx, svc, tk.Key, 0); err != nil {
		t.Fatal(err)
	}
	cs, _ := store.ListComments(svc.DB, tk.ID)
	if len(cs) != 1 || cs[0].Author != store.AuthorSystem {
		t.Fatalf("want one system comment, got %+v", cs)
	}
	for _, want := range []string{"PR review on https://github.com/o/r/pull/7:", "> a.go:3 (bob)\nnil deref", "please rename"} {
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
	if st.SeenCommentID != 13 || st.Unresolved != 1 || st.Review != "CHANGES_REQUESTED" {
		t.Errorf("state %+v", st)
	}

	// Second check: nothing new, no comment, next backoff step (30m).
	if _, err := ReviewCheck(ctx, svc, tk.Key, 0); err != nil {
		t.Fatal(err)
	}
	if cs, _ = store.ListComments(svc.DB, tk.ID); len(cs) != 1 {
		t.Fatalf("second check commented: %d comments", len(cs))
	}
	a, err := store.GetAutomation(svc.DB, "review-check:"+tk.Key)
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

func TestBackoffSchedule(t *testing.T) {
	fakeAPI(t, `[]`, `[]`, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]}}}}}`)
	svc, tk := reviewTask(t)
	for step, want := range backoff {
		if _, err := ReviewCheck(context.Background(), svc, tk.Key, step); err != nil {
			t.Fatal(err)
		}
		a, err := store.GetAutomation(svc.DB, "review-check:"+tk.Key)
		if err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		if d := time.Until(mustTime(t, *a.RunAt)); d < want-time.Minute || d > want+time.Minute {
			t.Errorf("step %d: next in %v, want %v", step, d, want)
		}
	}
	store.DeleteAutomation(svc.DB, "review-check:"+tk.Key)
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
	a, err := store.GetAutomation(db, "review-check:"+tk.Key)
	if err != nil {
		t.Fatal(err)
	}
	first := *a.RunAt
	sync(t, db, tk) // same head: untouched
	if a, _ = store.GetAutomation(db, "review-check:"+tk.Key); *a.RunAt != first {
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
