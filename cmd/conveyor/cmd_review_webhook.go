package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/gh"
)

func init() {
	register("review-webhook", runReviewWebhook, "opt-in review webhooks: forward PR review events to review checks ([--status|--stop])")
}

func pidFile(home string) string { return filepath.Join(home, "review-webhook.pid") }

// runningPID returns the live listener's pid, or 0.
func runningPID(home string) int {
	b, err := os.ReadFile(pidFile(home))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid > 0 && syscall.Kill(pid, 0) == nil {
		return pid
	}
	return 0
}

func runReviewWebhook(home string, args []string) error {
	fs := flag.NewFlagSet("review-webhook", flag.ContinueOnError)
	status := fs.Bool("status", false, "exit 0 when the listener is running")
	stop := fs.Bool("stop", false, "stop the listener")
	serve := fs.Bool("serve", false, "run in the foreground (what the default start launches)")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	pid := runningPID(home)
	switch {
	case *status:
		if pid == 0 {
			return errors.New("review-webhook is not running")
		}
		fmt.Fprintf(out, "running (pid %d)\n", pid)
		return nil
	case *stop:
		if pid == 0 {
			return errors.New("review-webhook is not running")
		}
		return syscall.Kill(pid, syscall.SIGTERM)
	case *serve:
		if pid != 0 {
			return fmt.Errorf("review-webhook already running (pid %d)", pid)
		}
		if err := os.WriteFile(pidFile(home), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			return err
		}
		defer os.Remove(pidFile(home))
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return withService(home, func(s *core.Service) error { return gh.RunWebhook(ctx, s.DB) })
	}
	// Start: check up front so a missing extension shows as an error, then detach.
	if pid != 0 {
		fmt.Fprintf(out, "already running (pid %d)\n", pid)
		return nil
	}
	if err := gh.CheckWebhookExt(); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(home, "logs"), 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(home, "logs", "review-webhook.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	c := exec.Command(self, "--home", home, "review-webhook", "--serve")
	c.Stdout, c.Stderr = logf, logf
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return err
	}
	fmt.Fprintf(out, "started (pid %d), log logs/review-webhook.log\n", c.Process.Pid)
	return c.Process.Release()
}
