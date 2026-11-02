// Package apps bundles the app manifests Conveyor offers to install.
package apps

import _ "embed"

// ReviewWebhooks is the opt-in review-webhooks app (docs/epic-builder.md §10).
//
//go:embed review-webhooks.json
var ReviewWebhooks string
