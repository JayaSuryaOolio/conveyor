// Package skills embeds the skills Conveyor installs in its plugin.
package skills

import "embed"

//go:embed all:epic all:conveyor all:conveyor-drift
var FS embed.FS
