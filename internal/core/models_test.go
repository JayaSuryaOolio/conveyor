package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModelsFromTranscripts(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	s := &Service{Home: t.TempDir(), ProjectsDir: t.TempDir()}
	if ml := s.Models(false); ml.Err == "" || len(ml.Models) != 0 {
		t.Fatalf("empty projects dir: want error, got %+v", ml)
	}
	if w, err := s.Models(false).Check("anything"); err != nil || !strings.Contains(w, "detection failed") {
		t.Fatalf("fallback: warning %q err %v", w, err)
	}
	dir := filepath.Join(s.ProjectsDir, "p")
	os.MkdirAll(dir, 0o755)
	lines := `{"message":{"model":"claude-opus-5"}}
{"message":{"model":"claude-opus-5-5"}}
{"message":{"model":"<synthetic>"}}
{"message":{"model":"sonnet"}}
{"message":{"model":"claude-haiku-4-5-20251001"}}
{"message":{"model":"claude-3-5-sonnet-20241022"}}
`
	os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(lines), 0o644)
	old := filepath.Join(dir, "old.jsonl")
	os.WriteFile(old, []byte(`{"model":"claude-opus-1"}`), 0o644)
	os.Chtimes(old, time.Now().Add(-60*24*time.Hour), time.Now().Add(-60*24*time.Hour))

	ml := s.Models(false)
	var got []string
	for _, m := range ml.Models {
		got = append(got, m.ID+":"+m.Tier+map[bool]string{true: "*"}[m.Latest])
	}
	want := "claude-haiku-4-5-20251001:haiku* claude-opus-5-5:opus* claude-opus-5:opus claude-3-5-sonnet-20241022:sonnet*"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v\nwant %s", got, want)
	}
	if r := ml.Resolve("opus"); r != "claude-opus-5-5" {
		t.Errorf("Resolve(opus) = %q", r)
	}
	if r := ml.Resolve("claude-opus-5[1m]"); r != "claude-opus-5" {
		t.Errorf("Resolve([1m]) = %q", r)
	}
	for _, ok := range []string{"", "inherit", "opus", "haiku", "claude-opus-5", "claude-haiku-4-5"} {
		if _, err := ml.Check(ok); err != nil {
			t.Errorf("Check(%q): %v", ok, err)
		}
	}
	if _, err := ml.Check("claude-opus-5-6"); err == nil || !strings.Contains(err.Error(), `did you mean "claude-opus-5-5"`) {
		t.Errorf("typo: %v", err)
	}
	if _, err := ml.Check("opsu"); err == nil || !strings.Contains(err.Error(), `did you mean "opus"`) {
		t.Errorf("alias typo: %v", err)
	}

	// Cached: a new transcript is not seen until refresh.
	os.WriteFile(filepath.Join(dir, "b.jsonl"), []byte(`{"model":"claude-fable-5-1"}`), 0o644)
	if s.Models(false).Resolve("fable") != "" {
		t.Error("cache not used")
	}
	if s.Models(true).Resolve("fable") != "claude-fable-5-1" {
		t.Error("refresh did not re-detect")
	}
}

func TestModelsFromAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("x-api-key") != "k" {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data":[{"id":"claude-sonnet-5-5","type":"model"},{"id":"claude-sonnet-5"}],"has_more":false}`))
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	s := &Service{Home: t.TempDir(), ProjectsDir: t.TempDir()}
	ml := s.Models(false)
	if ml.Source != "api" || ml.Resolve("sonnet") != "claude-sonnet-5-5" {
		t.Fatalf("api: %+v", ml)
	}
	t.Setenv("ANTHROPIC_API_KEY", "bad")
	if ml := s.Models(true); !strings.Contains(ml.Err, "401") {
		t.Fatalf("bad key: %+v", ml)
	}
}
