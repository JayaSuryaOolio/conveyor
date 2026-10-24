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
