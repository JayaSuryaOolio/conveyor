// Package skills embeds the skills Conveyor installs in its plugin.
package skills

import "embed"

//go:embed all:epic all:conveyor
var FS embed.FS
