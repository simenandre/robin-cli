package skills

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SkillDirName is the per-tool subdirectory the SKILL.md is written into.
const SkillDirName = "robin-cli"

// InstallTo writes SKILL.md for the given agent at the requested scope.
// projectRoot is used when scope is "project"; for "user" the file goes
// under the user's home directory. Returns an error when the destination
// already exists and force is false.
func InstallTo(out io.Writer, agentSlug, scope, projectRoot string, force bool) error {
	agent, err := lookupAgent(agentSlug)
	if err != nil {
		return err
	}
	skillDir, err := resolveSkillDir(agent, scope, projectRoot)
	if err != nil {
		return err
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")

	if _, err := os.Stat(skillFile); err == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", skillFile)
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("create skill directory %s: %w", skillDir, err)
	}
	if err := os.WriteFile(skillFile, []byte(SkillMD()), 0o644); err != nil { //nolint:gosec
		return fmt.Errorf("write SKILL.md: %w", err)
	}
	fmt.Fprintf(out, "  %-22s %s\n", agent.Name+" skill:", skillFile)
	return nil
}

// UninstallFrom removes SKILL.md for the given agent at the given scope.
// Returns nil when the file is already absent. The parent skill directory
// is removed when it ends up empty.
func UninstallFrom(out io.Writer, agentSlug, scope, projectRoot string) error {
	agent, err := lookupAgent(agentSlug)
	if err != nil {
		return err
	}
	skillDir, err := resolveSkillDir(agent, scope, projectRoot)
	if err != nil {
		return err
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.Remove(skillFile); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove %s: %w", skillFile, err)
	}
	_ = os.Remove(skillDir)
	fmt.Fprintf(out, "  %-22s removed (%s)\n", agent.Name+" skill:", skillFile)
	return nil
}

// ResolveSkillFile returns the path SKILL.md would be written to for the
// given agent at the given scope. Used by `robin skills list` so callers can
// show where the file lives without reading its contents.
func ResolveSkillFile(agentSlug, scope, projectRoot string) (string, error) {
	agent, err := lookupAgent(agentSlug)
	if err != nil {
		return "", err
	}
	dir, err := resolveSkillDir(agent, scope, projectRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "SKILL.md"), nil
}

func resolveSkillDir(agent Agent, scope, projectRoot string) (string, error) {
	switch scope {
	case "project":
		root := projectRoot
		if root == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("resolve working directory: %w", err)
			}
			root = cwd
		}
		return filepath.Join(root, agent.ProjectDir, SkillDirName), nil
	case "user":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, agent.UserDir, SkillDirName), nil
	}
	return "", fmt.Errorf("invalid scope %q (want project or user)", scope)
}

func lookupAgent(slug string) (Agent, error) {
	for _, a := range SupportedAgents {
		if a.Slug == slug {
			return a, nil
		}
	}
	return Agent{}, fmt.Errorf("unknown agent %q (supported: %s)", slug, supportedAgentSlugs())
}

func supportedAgentSlugs() string {
	out := ""
	for i, a := range SupportedAgents {
		if i > 0 {
			out += ", "
		}
		out += a.Slug
	}
	return out
}
