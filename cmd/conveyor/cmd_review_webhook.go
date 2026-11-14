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
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/gh"
)

func init() {
	register("review-webhook", runReviewWebhook, "opt-in review webhooks: forward PR review events to review checks ([--status|--stop])")
}

// defaultConnector is the id the built-in start (an open epic) runs under; the bundled manifest's slug matches it.
const defaultConnector = "review-webhooks"

func pidFile(home, id string) string { return filepath.Join(home, "connectors", id+".pid") }

// runningPID returns the live listener's pid, or 0. It also reads the pre-rebrand apps/<id>.pid,
// so a listener started by an older build is still found.
func runningPID(home, id string) int {
	b, err := os.ReadFile(pidFile(home, id))
	if err != nil {
		if b, err = os.ReadFile(filepath.Join(home, "apps", id+".pid")); err != nil {
			return 0
		}
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
	connector := fs.String("connector", defaultConnector, "connector id: names the pid file and logs/app/<id>.log (${{ connector.id }})")
	fs.StringVar(connector, "app", defaultConnector, "alias of --connector")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	app := connector
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(*app) {
		return fmt.Errorf("invalid --connector %q", *app)
	}
	pid := runningPID(home, *app)
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
		if err := os.MkdirAll(filepath.Dir(pidFile(home, *app)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(pidFile(home, *app), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			return err
		}
		defer os.Remove(pidFile(home, *app))
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return withService(home, func(s *core.Service) error { return gh.RunWebhook(ctx, s.DB, s.EnabledReviewWebhookEvents(*app)) })
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
	logRel := filepath.Join("logs", "app", *app+".log")
	logf, err := core.OpenLog(filepath.Join(home, logRel))
	if err != nil {
		return err
	}
	defer logf.Close()
	c := exec.Command(self, "--home", home, "review-webhook", "--connector", *app, "--serve")
	c.Stdout, c.Stderr = logf, logf
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return err
	}
	fmt.Fprintf(out, "started (pid %d), log %s\n", c.Process.Pid, logRel)
	return c.Process.Release()
}
