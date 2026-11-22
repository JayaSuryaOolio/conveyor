package ui

import (
	"context"
	"database/sql"
	"log"
	"os"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// consumeIntents hands each fresh CLI intent to emit once and drops stale ones unread
// (docs/ui-control-spec.md §2). now and emit are injected so tests need no window.
func consumeIntents(db *sql.DB, now time.Time, emit func(store.Intent)) {
	is, err := store.ConsumeIntents(db, now)
	if err != nil {
		log.Printf("ui intents: %v", err)
		return
	}
	for _, i := range is {
		emit(i)
	}
}

// onDataChange runs inside pollDataVersion: deliver intents first, then tell the frontend to refetch.
func (a *App) onDataChange(ctx context.Context) {
	consumeIntents(a.svc.DB, time.Now(), func(i store.Intent) {
		runtime.WindowUnminimise(ctx)
		runtime.WindowShow(ctx)
		runtime.EventsEmit(ctx, "ui:intent", i)
	})
	runtime.EventsEmit(ctx, "db:changed")
}

// SetView records the current view in <home>/ui-state.json for `conveyor ui current` and the `@` token.
// A file, not a DB row: navigating must not bump data_version and refetch everywhere.
func (a *App) SetView(v core.UIView) error {
	return core.WriteUIState(a.svc.Home, v, os.Getpid(), time.Now())
}
