package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/hook"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() { register("hook", runHook, "handle a Claude Code hook event (internal)") }

// runHook never returns an error: Claude Code must not be blocked. Failures go to logs/hook.log.
func runHook(home string, args []string) error {
	err := doHook(home, args)
	if err != nil {
		logHook(home, err)
	}
	return nil
}

func doHook(home string, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: conveyor hook <Event>")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	db, err := store.Open(core.DBPath(home))
	if err != nil {
		return err
	}
	defer db.Close()
	return hook.Handle(db, home, args[0], raw)
}

func logHook(home string, err error) {
	dir := filepath.Join(home, "logs")
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	f, e := os.OpenFile(filepath.Join(dir, "hook.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if e != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %v\n", time.Now().UTC().Format(time.RFC3339), err)
}
