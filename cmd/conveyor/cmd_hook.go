package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/hook"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
)

func init() { register("hook", runHook, "handle a Claude Code hook event (internal)") }

// runHook never returns an error: Claude Code must not be blocked. Failures go to logs/hook.log.
// Every event but PreToolUse (one per tool call, too noisy) leaves a line, so "no Stop for that session" is checkable.
func runHook(home string, args []string) (err error) {
	var raw []byte
	defer func() {
		if r := recover(); r != nil { // a panic would otherwise vanish into Claude Code's hook output
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
		if err != nil || len(args) != 1 || args[0] != "PreToolUse" {
			logHook(home, args, raw, err)
		}
		err = nil
	}()
	if len(args) != 1 {
		return fmt.Errorf("usage: conveyor hook <Event>")
	}
	raw, err = io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return doHook(home, args, raw)
}

func doHook(home string, args []string, raw []byte) error {
	if args[0] == "PreToolUse" { // the only hook that answers on stdout
		var openErr error
		res := hook.PreToolUse(raw, func() string {
			db, err := store.Open(core.DBPath(home))
			if err != nil {
				openErr = err
				return ""
			}
			defer db.Close()
			return store.GetSetting(db, "pr_comment_prefix")
		})
		fmt.Fprintf(out, "%s", res)
		return openErr
	}
	db, err := store.Open(core.DBPath(home))
	if err != nil {
		return err
	}
	defer db.Close()
	return hook.Handle(db, home, args[0], raw)
}

func logHook(home string, args []string, raw []byte, err error) {
	var p struct {
		SessionID string `json:"session_id"`
		Cwd       string `json:"cwd"`
	}
	json.Unmarshal(raw, &p) // best effort: a bad payload still gets its line, with the error
	key := workspace.KeyFromCwd(home, p.Cwd)
	if err == nil && key == "" {
		return // user-level hooks fire for every Claude session; only ours are worth a line
	}
	f, e := core.OpenLog(core.HookLogPath(home))
	if e != nil {
		return
	}
	defer f.Close()
	res := "ok"
	if err != nil {
		res = err.Error()
	}
	fmt.Fprintf(f, "%s %s %v session=%s cwd=%s: %s\n", time.Now().UTC().Format(time.RFC3339), key, args, p.SessionID, p.Cwd, res)
}
