// Package docs holds the text the sentinel prints on request. The files carry
// no YAML front matter, so nothing auto-loads them as an agent skill.
package docs

import _ "embed"

// Guide is the agent-facing guide, printed by "sentinel skill".
//
//go:embed agents.md
var Guide string

// Recipes are the worked examples, printed by "sentinel skill -recipes".
//
//go:embed recipes.md
var Recipes string
