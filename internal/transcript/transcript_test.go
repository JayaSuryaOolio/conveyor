package transcript

import "testing"

func TestSum(t *testing.T) {
	const f = "testdata/sample.jsonl"
	got, err := Sum(f, "second prompt")
	if err != nil {
		t.Fatal(err)
	}
	want := Usage{In: 12, Out: 12, CacheRead: 400, CacheWrite: 20, CtxPeak: 302, Calls: 2}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	all, _ := Sum(f, "")
	if all.Calls != 3 || all.CtxPeak != 1001 {
		t.Fatalf("whole file %+v", all)
	}
	// unknown prompt counts the whole file
	if u, _ := Sum(f, "nope"); u != all {
		t.Fatalf("%+v", u)
	}
	if _, err := Sum("testdata/missing", ""); err == nil {
		t.Fatal("want error")
	}
}

func TestCountWaste(t *testing.T) {
	const f = "testdata/waste.jsonl"
	got, err := CountWaste(f, "run prompt")
	if err != nil {
		t.Fatal(err)
	}
	// the read of /a.go before the run prompt does not count; the run's second read does
	if got.HookDenials != 1 || got.Rereads != 1 || got.DupBash != 1 || got.BigResults != 1 {
		t.Fatalf("got %+v", got)
	}
	if len(got.Failures) != 2 || got.Failures["hook PreToolUse:Bash blocked"] != 1 || got.Failures["tool Bash"] != 1 {
		t.Fatalf("failures %v", got.Failures)
	}
	if all, _ := CountWaste(f, ""); all.Rereads != 2 {
		t.Fatalf("whole file %+v", all)
	}
	if _, err := CountWaste("testdata/missing", ""); err == nil {
		t.Fatal("want error")
	}
}
