package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	lines := `{"type":"user","timestamp":"t1","message":{"content":"fix it"}}
{"type":"user","message":{"content":"<system-reminder>x</system-reminder>"}}
{"type":"assistant","timestamp":"t2","message":{"content":[{"type":"thinking"},{"type":"text","text":"On it."},{"type":"tool_use","name":"Bash","input":{"command":"go test ./...\nmore"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"a/b.go"}}]}}
`
	if err := os.WriteFile(p, []byte(lines+`{"type":"assis`), 0o644); err != nil {
		t.Fatal(err)
	}
	es, next, err := Log(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{"user", "fix it", "t1"}, {"text", "On it.", "t2"}, {"tool", "Bash: go test ./... …", "t2"}, {"tool", "Edit: a/b.go", ""}}
	if len(es) != len(want) {
		t.Fatalf("got %+v", es)
	}
	for i := range want {
		if es[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, es[i], want[i])
		}
	}
	if next != int64(len(lines)) {
		t.Fatalf("next %d, want %d", next, len(lines))
	}
	if es, n2, _ := Log(p, next); len(es) != 0 || n2 != next {
		t.Fatalf("partial line consumed: %+v %d", es, n2)
	}
}
