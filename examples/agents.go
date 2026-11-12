package examples

import (
	"embed"
	"io/fs"
)

//go:embed all:agent-plugin/agents/*.md
var rawAgentsFS embed.FS

// AgentsFS holds the seed software engineering agents bundled with Conveyor.
var AgentsFS fs.FS

func init() {
	sub, err := fs.Sub(rawAgentsFS, "agent-plugin/agents")
	if err != nil {
		panic(err)
	}
	AgentsFS = sub
}
