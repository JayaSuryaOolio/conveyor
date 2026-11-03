// Package skills embeds the skills Conveyor installs with its user hooks.
package skills

import "embed"

//go:embed all:epic
var FS embed.FS
