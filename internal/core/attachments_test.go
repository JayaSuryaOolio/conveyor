package core

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAttachment(t *testing.T) {
	home := t.TempDir()
	b := base64.StdEncoding.EncodeToString([]byte("png"))
	p, err := SaveAttachment(home, "K-1", "../my shot.PNG", b)
	if err != nil || filepath.Dir(p) != filepath.Join(home, "workspaces", "K-1", "attachments") || !strings.HasSuffix(p, "-my_shot.PNG") {
		t.Fatalf("save: %q %v", p, err)
	}
	if got, _ := os.ReadFile(p); string(got) != "png" {
		t.Fatalf("content %q", got)
	}
	if p, err = SaveAttachment(home, "", "a.webp", b); err != nil || filepath.Dir(p) != filepath.Join(home, "attachments") {
		t.Fatalf("no key: %q %v", p, err)
	}
	if _, err = SaveAttachment(home, "K-1", "a.svg", b); err == nil {
		t.Fatal("svg accepted")
	}
	if _, err = SaveAttachment(home, "K-1", "a.png", base64.StdEncoding.EncodeToString(make([]byte, maxAttachment+1))); err == nil {
		t.Fatal("oversize accepted")
	}
}

func TestAttachmentHandler(t *testing.T) {
	home := t.TempDir()
	p, _ := SaveAttachment(home, "", "a.png", base64.StdEncoding.EncodeToString([]byte("png")))
	os.WriteFile(filepath.Join(home, "secret.txt"), []byte("x"), 0o644)
	svg := filepath.Join(home, "workspaces", "K-1", "approach.svg")
	os.MkdirAll(filepath.Dir(svg), 0o755)
	os.WriteFile(svg, []byte("<svg/>"), 0o644)
	for u, want := range map[string]int{
		"/attachments" + p:                             http.StatusOK,
		"/attachments" + svg:                           http.StatusOK,
		"/attachments" + home + "/secret.txt":          http.StatusNotFound,
		"/attachments" + home + "/../x.png":            http.StatusNotFound,
		"/attachments" + filepath.Dir(home) + "/x.png": http.StatusNotFound,
	} {
		w := httptest.NewRecorder()
		AttachmentHandler(home).ServeHTTP(w, httptest.NewRequest("GET", u, nil))
		if w.Code != want {
			t.Errorf("%s: %d want %d", u, w.Code, want)
		}
	}
}

// TestAttachmentSVG: the Approach diagram (approach.svg next to BRIEF.md) is served
// as an image with scripts blocked (AC6).
func TestAttachmentSVG(t *testing.T) {
	home := t.TempDir()
	svg := filepath.Join(home, "workspaces", "K-1", "approach.svg")
	os.MkdirAll(filepath.Dir(svg), 0o755)
	os.WriteFile(svg, []byte("<svg/>"), 0o644)
	w := httptest.NewRecorder()
	AttachmentHandler(home).ServeHTTP(w, httptest.NewRequest("GET", "/attachments"+svg, nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("svg: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'none'") {
		t.Fatal("svg served without a no-script CSP")
	}
}
