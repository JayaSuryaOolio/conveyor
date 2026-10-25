package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
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
	if f, err := core.OpenLog(core.DispatcherLogPath(home)); err == nil {
		defer f.Close()
		w = io.MultiWriter(os.Stderr, f)
	} else {
		fmt.Fprintf(os.Stderr, "dispatcher log: %v\n", err)
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
	d.Log.Printf("started (daemon)")
	err = d.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
