// Package plugins makes the Orbit agent skill available to the binary.
// The skill's source of truth stays the plugin directory the Claude Code
// and Codex marketplaces serve; this package embeds that directory so
// `orbit skill install` can place a copy without a network fetch.
package plugins

import (
	"embed"
	"io/fs"
)

//go:embed all:orbit/skills/orbit
var skillTree embed.FS

// OrbitSkill returns the agent skill rooted at the directory holding its
// SKILL.md, the layout coding agents load from <skills dir>/orbit/.
func OrbitSkill() fs.FS {
	skill, err := fs.Sub(skillTree, "orbit/skills/orbit")
	if err != nil {
		panic("embedded Orbit skill is missing: " + err.Error())
	}
	return skill
}
