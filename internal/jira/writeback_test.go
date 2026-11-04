package jira

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// fakeWriteAcli logs every call's args to <dir>/argv. search answers <dir>/<parent key>.json,
// `comment create` returns id c1, and a file "notrans" makes transitions fail like a missing workflow step.
func fakeWriteAcli(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
echo "$*" >> ` + dir + `/argv
case "$3 $4" in
"transition "*) [ -f ` + dir + `/notrans ] && { echo "No allowed transitions found for given status" >&2; exit 1; }; exit 0;;
"comment create") echo '{"id":"c1"}'; exit 0;;
"comment update") exit 0;;
esac
k=${5#parent = }; k=${k%% *}
cat ` + dir + `/$k.json 2>/dev/null || echo '[]'
`
	os.WriteFile(filepath.Join(dir, "acli"), []byte(script), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

func calls(t *testing.T, dir string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(dir, "argv"))
	return string(b)
}

// wbEnv pulls OS-10 and OS-11 under epic "e" and attaches the write-back hook.
func wbEnv(t *testing.T, statusMap string) (*core.Service, store.Task, string) {
	t.Helper()
	dir := fakeWriteAcli(t)
	svc, epic := pullEnv(t, "a")
	if statusMap != "" {
		store.SetJiraSource(svc.DB, store.JiraSource{EpicID: epic.ID, JiraKey: "OS-1", DefaultRepo: "a", StatusMap: statusMap})
	}
	respond(t, dir, "OS-1", `[{"key":"OS-10","fields":{"summary":"Ten"}},{"key":"OS-11","fields":{"summary":"Eleven"}}]`)
	if _, err := Pull(context.Background(), svc, epic.Key); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "argv"))
	Attach(svc)
	t.Cleanup(func() { Wait(); store.OnTaskChange = nil })
	return svc, epic, dir
}

func move(t *testing.T, svc *core.Service, ext string, to store.Status, actor store.Actor) {
	t.Helper()
	if _, err := store.UpdateTaskStatus(svc.DB, byExt(t, svc, ext).ID, to, actor); err != nil {
		t.Fatal(err)
	}
	Wait()
}

// AC5: default map, per-source override, unavailable transition logged on the source and not fatal.
func TestWriteBackTransitions(t *testing.T) {
	svc, epic, dir := wbEnv(t, `{"running":"Doing"}`)
	move(t, svc, "OS-10", store.StatusTodo, store.ActorHuman)
	move(t, svc, "OS-10", store.StatusRunning, store.ActorSystem)
	got := calls(t, dir)
	if !strings.Contains(got, "workitem transition --key OS-10 --status To Do --yes") ||
		!strings.Contains(got, "workitem transition --key OS-10 --status Doing --yes") {
		t.Fatalf("calls: %s", got)
	}

	os.WriteFile(filepath.Join(dir, "notrans"), nil, 0o644)
	move(t, svc, "OS-10", store.StatusReview, store.ActorSystem) // must not error
	if src, _ := store.GetJiraSource(svc.DB, epic.ID); !strings.Contains(src.LastError, "No allowed transitions") {
		t.Fatalf("not logged on source: %+v", src)
	}
}

// AC6: one comment, created once, then edited in place by id; unchanged content makes no call.
func TestWriteBackPRComment(t *testing.T) {
	svc, _, dir := wbEnv(t, "")
	tk := byExt(t, svc, "OS-10")
	set := func(url string, state *string) {
		r := tk.Repos[0]
		r.PRURL, r.PRState = &url, state
		if err := store.UpdateTaskRepo(svc.DB, r); err != nil {
			t.Fatal(err)
		}
		Wait()
	}
	set("https://gh/pr/1", nil)
	merged := "MERGED"
	set("https://gh/pr/1", &merged) // PR poller refresh: same body
	if got := calls(t, dir); strings.Count(got, "comment create --key OS-10") != 1 || strings.Contains(got, "comment update") ||
		!strings.Contains(got, "https://gh/pr/1") {
		t.Fatalf("calls: %s", got)
	}
	set("https://gh/pr/2", nil)
	if got := calls(t, dir); strings.Count(got, "comment create") != 1 || !strings.Contains(got, "comment update --key OS-10 --id c1") {
		t.Fatalf("calls: %s", got)
	}
}

// AC7: epic goes Done once every synced task is done and Jira has no unsynced open child.
func TestWriteBackEpicCompletion(t *testing.T) {
	svc, epic, dir := wbEnv(t, "")
	svc.DB.Exec(`UPDATE tasks SET status='review' WHERE external_key IN ('OS-10','OS-11')`)
	epicDone := func() bool { return strings.Contains(calls(t, dir), "transition --key OS-1 --status Done") }

	move(t, svc, "OS-10", store.StatusDone, store.ActorHuman)
	if epicDone() {
		t.Fatal("epic completed with OS-11 open")
	}

	respond(t, dir, "OS-1", `[{"key":"OS-10","fields":{}},{"key":"OS-11","fields":{}},{"key":"OS-99","fields":{}}]`) // unsynced open child
	move(t, svc, "OS-11", store.StatusDone, store.ActorHuman)
	if src, _ := store.GetJiraSource(svc.DB, epic.ID); epicDone() || src.Done {
		t.Fatalf("epic completed with unsynced open child: %+v", src)
	}

	respond(t, dir, "OS-1", `[{"key":"OS-10","fields":{}},{"key":"OS-11","fields":{}}]`)
	if err := Complete(context.Background(), svc, epic.ID); err != nil {
		t.Fatal(err)
	}
	if src, _ := store.GetJiraSource(svc.DB, epic.ID); !epicDone() || !src.Done {
		t.Fatalf("epic not completed: %+v\n%s", src, calls(t, dir))
	}
}
