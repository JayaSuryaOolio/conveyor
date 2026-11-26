// Package playbook names the workflows a task can follow, chosen by a playbook:<name> label.
// A playbook is a file in skills/epic/planners (planner == playbook); no label means Default.
package playbook

import (
	"slices"
	"strings"
)

const (
	Prefix  = "playbook:"
	Dir     = "skills/epic/planners" // one <name>.md per playbook
	Default = "default"              // architect -> implement -> review, the loop every brief already carries
)

var names = []string{Default, "wayfinder", "superpowers", "pstack", "impeccable"}

// List is every playbook, Default first.
func List() []string { return slices.Clone(names) }

// Of is the playbook named by the labels; no label or an unknown name gives Default.
func Of(labels []string) string {
	for _, l := range labels {
		if n, ok := strings.CutPrefix(l, Prefix); ok && slices.Contains(names, n) {
			return n
		}
	}
	return Default
}
