package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelsBuiltinFallback(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	ml := (&Service{}).Models()
	if ml.Source != "builtin" {
		t.Fatalf("source %q", ml.Source)
	}
	var got []string
	for _, m := range ml.Models {
		got = append(got, m.ID+":"+m.Tier+map[bool]string{true: "*"}[m.Latest])
	}
	want := "claude-fable-5-1:fable* claude-haiku-5-5:haiku* claude-opus-5-5:opus* claude-sonnet-5-5:sonnet*"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v\nwant %s", got, want)
	}
	if r := ml.Resolve("opus"); r != "claude-opus-5-5" {
		t.Errorf("Resolve(opus) = %q", r)
	}
	if r := ml.Resolve("claude-fable-5-1[1m]"); r != "claude-fable-5-1" {
		t.Errorf("Resolve([1m]) = %q", r)
	}
	if r := ml.Resolve("claude-opus-9"); r != "" {
		t.Errorf("unknown resolved to %q", r)
	}
}

func TestClassifyLatestPerTier(t *testing.T) {
	ms := classify([]string{"claude-opus-5", "claude-opus-5-5", "claude-3-5-sonnet-20241022", "claude-haiku-4-5-20251001", "claude-haiku-5-5", "<junk>"})
	var got []string
	for _, m := range ms {
		got = append(got, m.ID+map[bool]string{true: "*"}[m.Latest])
	}
	want := "claude-haiku-5-5* claude-haiku-4-5-20251001 claude-opus-5-5* claude-opus-5 claude-3-5-sonnet-20241022*"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v\nwant %s", got, want)
	}
	if r := (ModelList{Models: ms}).Resolve("claude-haiku-4-5"); r != "claude-haiku-4-5-20251001" {
		t.Errorf("dateless id resolved to %q", r)
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
	ml := (&Service{}).Models()
	if ml.Source != "api" || len(ml.Models) != 2 || ml.Resolve("sonnet") != "claude-sonnet-5-5" {
		t.Fatalf("api: %+v", ml)
	}
	t.Setenv("ANTHROPIC_API_KEY", "bad") // API error: fall back to the builtin list
	if ml := (&Service{}).Models(); ml.Source != "builtin" {
		t.Fatalf("bad key: %+v", ml)
	}
}
