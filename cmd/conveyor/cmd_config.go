package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() { register("config", runConfig, "list, get or set settings") }

// isSecretKey reports whether a setting holds a credential (tokens, secrets, passwords, *_key).
func isSecretKey(k string) bool {
	return strings.Contains(k, "token") || strings.Contains(k, "secret") || strings.Contains(k, "password") || strings.HasSuffix(k, "_key")
}

// mask keeps the first 4 chars of a long value so tokens stay recognisable; short values are fully hidden.
func mask(v string) string {
	switch {
	case v == "":
		return ""
	case len(v) < 12:
		return "****"
	}
	return v[:4] + "****"
}

func runConfig(home string, args []string) error {
	reveal := false
	rest := args[:0:0]
	for _, a := range args {
		if a == "--reveal" {
			reveal = true
		} else {
			rest = append(rest, a)
		}
	}
	args = rest
	if len(args) == 0 {
		return fmt.Errorf("usage: conveyor config list [--reveal] | get k [--reveal] | set k v")
	}
	return withService(home, func(s *core.Service) error {
		switch {
		case args[0] == "list" && len(args) == 1:
			m := store.ListSettings(s.DB)
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := m[k]
				if !reveal && isSecretKey(k) {
					v = mask(v)
				}
				fmt.Fprintf(out, "%s=%s\n", k, v)
			}
			return nil
		case args[0] == "get" && len(args) == 2:
			fmt.Fprintln(out, store.GetSetting(s.DB, args[1]))
			return nil
		case args[0] == "set" && len(args) == 3:
			return store.SetSetting(s.DB, args[1], args[2])
		}
		return fmt.Errorf("usage: conveyor config list [--reveal] | get k [--reveal] | set k v")
	})
}
