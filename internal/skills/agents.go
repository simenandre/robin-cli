package skills

// Agent describes one AI tool the robin skill can be installed for. ProjectDir
// and UserDir are the directories the tool reads SKILL.md from at the
// per-project and per-user scopes respectively.
type Agent struct {
	Name       string
	Slug       string
	ProjectDir string
	UserDir    string
}

// SupportedAgents lists every AI tool we can install the robin skill for. The
// generic ".agents/skills" entry is a catch-all for tools that follow the
// emerging convention without a vendor-specific directory.
var SupportedAgents = []Agent{
	{Name: "Claude Code", Slug: "claude-code", ProjectDir: ".claude/skills", UserDir: ".claude/skills"},
	{Name: "Cursor", Slug: "cursor", ProjectDir: ".cursor/skills", UserDir: ".cursor/skills"},
	{Name: "VS Code (Copilot)", Slug: "vscode", ProjectDir: ".vscode/skills", UserDir: ".vscode/skills"},
	{Name: "Gemini CLI", Slug: "gemini-cli", ProjectDir: ".gemini/skills", UserDir: ".gemini/skills"},
	{Name: "OpenAI Codex", Slug: "codex", ProjectDir: ".codex/skills", UserDir: ".codex/skills"},
	{Name: "Any compatible agent (.agents/skills)", Slug: "generic", ProjectDir: ".agents/skills", UserDir: ".agents/skills"},
}
