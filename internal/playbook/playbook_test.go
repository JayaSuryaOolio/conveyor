package playbook

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPlaybooks(t *testing.T) {
	if got := Of([]string{"repo:x"}); got != Default {
		t.Fatalf("no label = %q, want %q", got, Default)
	}
	if got := Of([]string{"playbook:pstack"}); got != "pstack" {
		t.Fatalf("Of = %q", got)
	}
	if got := Of([]string{"playbook:nope"}); got != Default {
		t.Fatalf("unknown name = %q, want default", got)
	}
	if List()[0] != Default || !slices.Contains(List(), "wayfinder") {
		t.Fatalf("List = %v", List())
	}
	for _, n := range List() { // every playbook ships its adapter file
		if _, err := os.Stat(filepath.Join("..", "..", Dir, n+".md")); err != nil {
			t.Errorf("playbook %s: %v", n, err)
		}
	}
}
