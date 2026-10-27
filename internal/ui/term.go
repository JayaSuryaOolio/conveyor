package ui

import (
	"encoding/base64"
	"strconv"
	"strings"
	"sync"

	"github.com/JayaSuryaOolio/conveyor/internal/term"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// TermInfo describes one embedded terminal of a task.
type TermInfo struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Exited bool   `json:"exited"`
}

// TermBuf is a terminal's scrollback (base64) and the byte count it ends at.
type TermBuf struct {
	Data string `json:"data"`
	End  int64  `json:"end"`
}

var (
	termMu   sync.Mutex
	termsFor = map[string][]TermInfo{} // task key -> its terminals
)

// startTerms wires pty output to Wails events; chunks are "<end>:<base64>".
func (a *App) startTerms() {
	term.Output = func(id string, b []byte, end int64) {
		runtime.EventsEmit(a.ctx, "term:"+id, strconv.FormatInt(end, 10)+":"+base64.StdEncoding.EncodeToString(b))
	}
	term.Exit = func(id string) { runtime.EventsEmit(a.ctx, "term-exit:"+id) }
}

// TermStart opens an embedded terminal for the same cwd/command RunAction would use.
func (a *App) TermStart(key, kind, repo string) (TermInfo, error) {
	act, err := a.svc.FindAction(key, kind, repo)
	if err != nil {
		return TermInfo{}, err
	}
	title := act.Repo + " · shell"
	switch {
	case strings.HasPrefix(kind, "resume:"):
		id := []rune(strings.TrimPrefix(kind, "resume:"))
		title = "resume " + string(id[:min(4, len(id))]) + "…"
	case kind == "resume":
		title = "resume"
	case kind == "fresh":
		title = "new session"
	case kind == "attach" || strings.HasPrefix(kind, "attach:"):
		title = "attach"
	}
	id, err := term.Start(act.Cwd, act.Cmd)
	if err != nil {
		return TermInfo{}, err
	}
	info := TermInfo{ID: id, Title: title}
	termMu.Lock()
	termsFor[key] = append(termsFor[key], info)
	termMu.Unlock()
	return info, nil
}

// TermList returns the task's open terminals.
func (a *App) TermList(key string) []TermInfo {
	termMu.Lock()
	defer termMu.Unlock()
	out := []TermInfo{}
	for _, t := range termsFor[key] {
		t.Exited = term.Exited(t.ID)
		out = append(out, t)
	}
	return out
}

// TermWrite sends keystrokes to a terminal.
func (a *App) TermWrite(id, data string) error { return term.Write(id, data) }

// TermResize sets a terminal's size.
func (a *App) TermResize(id string, cols, rows int) error { return term.Resize(id, cols, rows) }

// TermBuffer returns the replay buffer.
func (a *App) TermBuffer(id string) (TermBuf, error) {
	b, end, err := term.Buffer(id)
	return TermBuf{base64.StdEncoding.EncodeToString(b), end}, err
}

// TermClose kills the terminal and removes its tab.
func (a *App) TermClose(key, id string) error {
	termMu.Lock()
	ts := termsFor[key]
	for i, t := range ts {
		if t.ID == id {
			termsFor[key] = append(ts[:i:i], ts[i+1:]...)
			break
		}
	}
	termMu.Unlock()
	return term.Close(id)
}
