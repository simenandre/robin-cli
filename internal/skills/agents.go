package skills

// Agent describes one AI tool the robin skill can be installed for. Dir is
// the per-agent skill directory under the user's home (e.g. ".claude/skills").
type Agent struct {
	Name string
	Slug string
	Dir  string
}

// SupportedAgents lists every AI tool we can install the robin skill for. The
// generic ".agents/skills" entry is a catch-all for tools that follow the
// emerging convention without a vendor-specific directory.
var SupportedAgents = []Agent{
	{Name: "Claude Code", Slug: "claude-code", Dir: ".claude/skills"},
	{Name: "Cursor", Slug: "cursor", Dir: ".cursor/skills"},
	{Name: "VS Code (Copilot)", Slug: "vscode", Dir: ".vscode/skills"},
	{Name: "Gemini CLI", Slug: "gemini-cli", Dir: ".gemini/skills"},
	{Name: "OpenAI Codex", Slug: "codex", Dir: ".codex/skills"},
	{Name: "Any compatible agent (.agents/skills)", Slug: "generic", Dir: ".agents/skills"},
}
