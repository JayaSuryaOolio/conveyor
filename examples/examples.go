// Package examples bundles the manifests Conveyor offers under "Templates". They double as templates for new apps.
package examples

import "embed"

// FS holds one <name>.json manifest per example app.
//
//go:embed *.json
var FS embed.FS
