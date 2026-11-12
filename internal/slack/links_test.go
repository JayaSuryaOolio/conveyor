package slack

import "testing"

func TestLinks(t *testing.T) {
	s := map[string]string{
		"max_parallel": "5", "slack_thread:C1:1.1": "CV-2", "slack_thread:C1:2.2": "CV-1",
		"slack_thread:C1:3.3": "CV-1", "slack_task:CV-1": "C1:2.2",
	}
	if got := StripLinks(s); len(got) != 1 || got["max_parallel"] != "5" {
		t.Fatal(got)
	}
	p, total := Links(s, 0, 2)
	if total != 3 || len(p) != 2 || p[0] != (Link{"CV-1", "C1", "2.2", true}) || p[1].Notify {
		t.Fatal(p, total)
	}
	if p, _ = Links(s, 2, 2); len(p) != 1 || p[0].Task != "CV-2" {
		t.Fatal(p)
	}
	if p, _ = Links(s, 9, 2); len(p) != 0 {
		t.Fatal(p)
	}
}
