package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// taskAttach replaces this process with `claude attach <bg_id>`.
func taskAttach(s *core.Service, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: conveyor task attach KEY")
	}
	t, err := store.GetTaskByKey(s.DB, args[0])
	if err != nil {
		return err
	}
	r, err := store.LatestRun(s.DB, t.ID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && r.BgID == nil {
		return fmt.Errorf("%s has no background session to attach to", t.Key)
	}
	if err != nil {
		return err
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude CLI not found: %w", err)
	}
	s.DB.Close() // Exec skips deferred closes
	return syscall.Exec(bin, []string{"claude", "attach", *r.BgID}, os.Environ())
}

func taskOpen(s *core.Service, args []string) error {
	fs := flag.NewFlagSet("task open", flag.ContinueOnError)
	shell := fs.Bool("shell", false, "open a shell in the worktree (default)")
	resume := fs.Bool("resume", false, "resume the latest session")
	fresh := fs.Bool("fresh", false, "new session from the brief")
	repo := fs.String("repo", "", "repo (shell; default primary)")
	print := fs.Bool("print", false, "print the command instead of running it")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: conveyor task open KEY [--shell|--resume|--fresh] [--repo r] [--print]")
	}
	kind, n := "shell", 0
	for k, on := range map[string]bool{"shell": *shell, "resume": *resume, "fresh": *fresh} {
		if on {
			kind, n = k, n+1
		}
	}
	if n > 1 {
		return fmt.Errorf("choose only one of --shell, --resume, --fresh")
	}
	a, err := s.FindAction(pos[0], kind, *repo)
	if err != nil {
		return err
	}
	if *print {
		fmt.Fprintln(out, s.Render(a))
		return nil
	}
	return s.Launch(a)
}
