package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/dispatch"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() { register("daemon", runDaemon, "run the dispatcher in the foreground") }

func runDaemon(home string, args []string) error {
	release, err := dispatch.Lock(home)
	if err != nil {
		return err // ErrLocked prints "dispatcher already running"
	}
	defer release()
	var w io.Writer = os.Stderr
	if err := os.MkdirAll(filepath.Join(home, "logs"), 0o755); err == nil {
		if f, err := os.OpenFile(filepath.Join(home, "logs", "dispatcher.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			defer f.Close()
			w = io.MultiWriter(os.Stderr, f)
		}
	}
	db, err := store.Open(core.DBPath(home))
	if err != nil {
		return err
	}
	defer db.Close()
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find conveyor binary: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	d := &dispatch.Dispatcher{DB: db, Home: home, ConveyorBin: bin, Log: log.New(w, "dispatcher: ", log.LstdFlags)}
	d.Log.Printf("started")
	err = d.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
