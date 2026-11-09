package apps

import (
	"io/fs"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/examples"
)

// Example is a bundled example app (examples/*.json), offered but never auto-installed.
type Example struct {
	File        string `json:"file"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ExampleApps lists the bundled examples.
func ExampleApps() []Example {
	var out []Example
	files, _ := fs.Glob(examples.FS, "*.json")
	for _, f := range files {
		raw, _ := examples.FS.ReadFile(f)
		if m, err := ParseManifest(string(raw)); err == nil {
			out = append(out, Example{strings.TrimSuffix(f, ".json"), m.Name, m.Description})
		}
	}
	return out
}
