package gh

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func post(h http.Handler, event, body string) int {
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestWebhookSchedulesCheckNow(t *testing.T) {
	svc, tk := reviewTask(t)
	h := WebhookHandler(svc.DB)

	if c := post(h, "pull_request_review", `{"pull_request":{"html_url":"https://github.com/o/r/pull/99"}}`); c != 204 {
		t.Fatalf("unknown PR: %d", c)
	}
	if c := post(h, "ping", `{}`); c != 204 {
		t.Fatalf("ping: %d", c)
	}
	if c := post(h, "pull_request_review_comment", `{"pull_request":{"html_url":"https://github.com/o/r/pull/7"}}`); c != 200 {
		t.Fatalf("review event: %d", c)
	}
	as, _ := store.ListAutomations(svc.DB)
	if len(as) != 1 || as[0].Name != "review-check:"+tk.Key || as[0].RunAt == nil {
		t.Fatalf("want one review-check, got %+v", as)
	}
	if at, _ := time.Parse(time.RFC3339, *as[0].RunAt); time.Since(at) > time.Minute || at.After(time.Now().Add(time.Second)) {
		t.Fatalf("not scheduled for now: %s", *as[0].RunAt)
	}
}

func TestWebhookRepos(t *testing.T) {
	svc, tk := reviewTask(t)
	if got := openPRRepos(svc.DB); len(got) != 1 || got[0] != "o/r" {
		t.Fatalf("got %v", got)
	}
	store.UpdateTaskStatus(svc.DB, tk.ID, store.StatusDone, store.ActorSystem)
	if got := openPRRepos(svc.DB); len(got) != 0 {
		t.Fatalf("done task forwarded: %v", got)
	}
}

func TestWebhookMissingExtension(t *testing.T) {
	dir := t.TempDir() // a gh that rejects `webhook`, like gh without the extension
	os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\necho 'unknown command \"webhook\"' >&2; exit 1\n"), 0o755)
	t.Setenv("PATH", dir)
	err := CheckWebhookExt()
	if !errors.Is(err, ErrNoWebhookExt) || !strings.Contains(err.Error(), "gh extension install cli/gh-webhook") {
		t.Fatalf("got %v", err)
	}
}
