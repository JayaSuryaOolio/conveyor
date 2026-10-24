// Package term runs pty-backed terminals for the UI. They live in this process,
// keep the last bufMax bytes for replay, and are killed on Close/CloseAll.
package term

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/creack/pty"
)

const bufMax = 256 << 10

// Output and Exit are set by the host before Start. Output gets each chunk and the
// total bytes produced so far including it, so a client can splice replay and live data.
var (
	Output func(id string, chunk []byte, end int64)
	Exit   func(id string)
)

type term struct {
	ptmx   *os.File
	cmd    *exec.Cmd
	mu     sync.Mutex
	buf    []byte
	end    int64 // total bytes ever written
	exited atomic.Bool
}

var (
	mu    sync.Mutex
	terms = map[string]*term{}
	next  int
)

func get(id string) (*term, error) {
	mu.Lock()
	defer mu.Unlock()
	if t := terms[id]; t != nil {
		return t, nil
	}
	return nil, fmt.Errorf("no terminal %q", id)
}

// Start runs a login shell in cwd. With cmd it runs `cmd; exec $SHELL -l`, so the
// shell stays after cmd ends.
func Start(cwd, cmd string) (string, error) {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	args := []string{"-l"}
	if cmd != "" {
		args = append(args, "-c", cmd+"; exec '"+strings.ReplaceAll(sh, "'", `'\''`)+"' -l")
	}
	c := exec.Command(sh, args...)
	c.Dir = cwd
	c.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(c, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		return "", err
	}
	t := &term{ptmx: ptmx, cmd: c}
	mu.Lock()
	next++
	id := fmt.Sprintf("t%d", next)
	terms[id] = t
	mu.Unlock()
	go t.pump(id)
	return id, nil
}

func (t *term) pump(id string) {
	b := make([]byte, 32<<10)
	for {
		n, err := t.ptmx.Read(b)
		if n > 0 {
			chunk := append([]byte(nil), b[:n]...)
			t.mu.Lock()
			t.buf = append(t.buf, chunk...)
			if len(t.buf) > bufMax {
				t.buf = append([]byte(nil), t.buf[len(t.buf)-bufMax:]...)
			}
			t.end += int64(n)
			end := t.end
			t.mu.Unlock()
			if Output != nil {
				Output(id, chunk, end)
			}
		}
		if err != nil {
			break
		}
	}
	t.cmd.Wait()
	t.exited.Store(true)
	if Exit != nil {
		Exit(id)
	}
}

// Write sends keystrokes to the terminal.
func Write(id, data string) error {
	t, err := get(id)
	if err != nil {
		return err
	}
	_, err = t.ptmx.WriteString(data)
	return err
}

// Resize sets the terminal size.
func Resize(id string, cols, rows int) error {
	t, err := get(id)
	if err != nil {
		return err
	}
	return pty.Setsize(t.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Buffer returns the replay buffer and the total bytes produced so far.
func Buffer(id string) ([]byte, int64, error) {
	t, err := get(id)
	if err != nil {
		return nil, 0, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]byte(nil), t.buf...), t.end, nil
}

// Exited reports whether the process has ended (the terminal stays until Close).
func Exited(id string) bool {
	t, err := get(id)
	return err != nil || t.exited.Load()
}

// Close kills the process and forgets the terminal.
func Close(id string) error {
	mu.Lock()
	t := terms[id]
	delete(terms, id)
	mu.Unlock()
	if t == nil {
		return fmt.Errorf("no terminal %q", id)
	}
	if t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}
	return t.ptmx.Close()
}

// CloseAll kills every terminal.
func CloseAll() {
	mu.Lock()
	ids := make([]string, 0, len(terms))
	for id := range terms {
		ids = append(ids, id)
	}
	mu.Unlock()
	for _, id := range ids {
		Close(id)
	}
}
