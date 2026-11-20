// Package skills embeds the skills Conveyor installs in its plugin.
package skills

import "embed"

//go:embed all:epic all:conveyor all:conveyor-drift all:write-migration
var FS embed.FS
