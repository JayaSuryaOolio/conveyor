package term

import (
	"bytes"
	"testing"
	"time"
)

func TestStartEcho(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	id, err := Start(t.TempDir(), "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	defer Close(id)
	for i := 0; i < 100; i++ {
		b, end, _ := Buffer(id)
		if bytes.Contains(b, []byte("hi")) && end == int64(len(b)) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no output")
}
