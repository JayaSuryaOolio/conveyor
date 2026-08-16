// Command conveyor is Conveyor's entrypoint: own chi router, own port, own SQLite DB.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: conveyor serve [--port N] [--db path]")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8420, "port to listen on")
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database file")
	fs.Parse(os.Args[2:])

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("conveyor listening on %s (db=%s)", addr, *dbPath)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "conveyor.db"
	}
	return filepath.Join(home, ".conveyor", "conveyor.db")
}
