// Package skills owns the canonical SKILL.md content shipped to AI tools
// (Claude Code, Codex, Cursor, ...) plus the install flow that writes it
// into their per-agent skill directories.
package skills

import _ "embed"

//go:embed SKILL.md
var skillMD string

// SkillMD returns the contents of the SKILL.md file that teaches AI agents
// how to use the robin CLI to discover and book Robin meeting rooms.
func SkillMD() string { return skillMD }
