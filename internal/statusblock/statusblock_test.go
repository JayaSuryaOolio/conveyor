package statusblock

import "testing"

const full = `Did the work.

<conveyor>
status: done
summary: Implemented it.
 More detail.
state: branch cv/CV-1-x, PR #4, tests: pass
Next: nothing
criteria:
- [x] AC1 — shown
- [ ] AC2 — why not
learning: first lesson
LEARNING: second lesson
spawn: child one
bogus: ignored
</conveyor>
trailing`

func TestFull(t *testing.T) {
	b, rest, ok := Parse(full)
	if !ok || b.Status != "done" {
		t.Fatalf("ok=%v b=%+v", ok, b)
	}
	if b.Summary != "Implemented it.\nMore detail." || b.State != "branch cv/CV-1-x, PR #4, tests: pass" || b.Next != "nothing" {
		t.Fatalf("%+v", b)
	}
	if len(b.Criteria) != 2 || !b.Criteria[0].Met || b.Criteria[1].Met || b.Criteria[1].Text != "AC2 — why not" {
		t.Fatalf("criteria %+v", b.Criteria)
	}
	if len(b.Learnings) != 2 || b.Learnings[1] != "second lesson" || len(b.Spawn) != 1 {
		t.Fatalf("%+v", b)
	}
	if rest != "Did the work.\n\n\ntrailing" {
		t.Fatalf("rest %q", rest)
	}
}

func TestNoBlockAndInvalid(t *testing.T) {
	for _, m := range []string{"plain", "<conveyor>\nsummary: x\n</conveyor>", "<conveyor>\nstatus: maybe\n</conveyor>"} {
		if _, rest, ok := Parse(m); ok || rest != m {
			t.Fatalf("%q ok=%v rest=%q", m, ok, rest)
		}
	}
}

func TestLastWins(t *testing.T) {
	b, rest, ok := Parse("<conveyor>\nstatus: failed\n</conveyor>\nmid\n<conveyor>\nstatus: DONE\n</conveyor>")
	if !ok || b.Status != "done" {
		t.Fatalf("%+v", b)
	}
	if rest != "<conveyor>\nstatus: failed\n</conveyor>\nmid" {
		t.Fatalf("rest %q", rest)
	}
}
