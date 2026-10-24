// Command conveyor is Conveyor's CLI. Command groups live in cmd_<group>.go
// and register themselves with register().
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// cmdFunc runs a command group with the resolved home and the args after its name.
type cmdFunc func(home string, args []string) error

type command struct {
	fn   cmdFunc
	help string
}

var commands = map[string]command{}

// register adds a command group; call it from an init() in cmd_<group>.go.
func register(name string, fn cmdFunc, help string) { commands[name] = command{fn, help} }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	out = stdout
	fs := flag.NewFlagSet("conveyor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	homeFlag := fs.String("home", "", "conveyor home (default $CONVEYOR_HOME or ~/.conveyor)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		usage(stderr)
		return 1
	}
	c, ok := commands[rest[0]]
	if !ok {
		fmt.Fprintf(stderr, "unknown command %q\n", rest[0])
		usage(stderr)
		return 1
	}
	home, err := core.Home(*homeFlag)
	if err == nil {
		err = c.fn(home, rest[1:])
	}
	if err != nil {
		if rest[0] == "hook" {
			return 0 // hooks never fail Claude
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: conveyor [--home dir] <command> [args]\n\ncommands:")
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %-10s %s\n", n, commands[n].help)
	}
}

// out is where commands print results (stdout, swappable in tests).
var out io.Writer = os.Stdout

// openService opens the DB under home.
func openService(home string) (*core.Service, error) {
	db, err := store.Open(core.DBPath(home))
	if err != nil {
		return nil, err
	}
	return &core.Service{DB: db, Home: home}, nil
}

// withService opens the service, runs fn and closes the DB.
func withService(home string, fn func(s *core.Service) error) error {
	s, err := openService(home)
	if err != nil {
		return err
	}
	defer s.DB.Close()
	return fn(s)
}

// parseArgs parses flags interspersed with positionals ("task new <title> --repo a").
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func splitCSV(s string) []string {
	var r []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			r = append(r, p)
		}
	}
	return r
}
