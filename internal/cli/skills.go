package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/simenandre/robin-cli/internal/skills"
	"github.com/spf13/cobra"
)

func newSkillsCmd(io *IO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Install the robin SKILL.md into AI tool skill directories.",
		Long: `Manage the robin SKILL.md that teaches AI agents (Claude Code,
Cursor, Codex, Gemini CLI, ...) when and how to call the robin CLI.

A skill is a single Markdown file with YAML frontmatter the agent reads
into its context. Installing it scopes the help to either this repo
(--scope project, default) or your user profile (--scope user), and
each agent has its own skill directory.

Run 'robin skills install' with no flags on an interactive terminal to
pick scope and agents step-by-step.`,
		Example: `  # Interactive — pick scope and agents
  robin skills install

  # Install for Claude Code in this repo
  robin skills install --agent claude-code

  # Install for several agents at once
  robin skills install --agent claude-code --agent codex

  # Install for all supported agents at the user scope
  robin skills install --all --scope user

  # Show where SKILL.md is (or would be) for each agent
  robin skills list`,
	}
	cmd.AddCommand(
		newSkillsInstallCmd(io),
		newSkillsUninstallCmd(io),
		newSkillsListCmd(io),
	)
	return cmd
}

func newSkillsInstallCmd(io *IO) *cobra.Command {
	var (
		agentSlugs []string
		all        bool
		scope      string
		force      bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write SKILL.md into one or more agents' skill directories.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			scopeSet := cmd.Flags().Changed("scope")
			if err := validateScope(scope); err != nil {
				return err
			}
			projectRoot, err := os.Getwd()
			if err != nil {
				return err
			}

			interactive := !io.NoInput && io.StdinTTY() &&
				len(agentSlugs) == 0 && !all
			var targets []string
			if interactive {
				if !scopeSet {
					scope, err = promptScope(io, "install")
					if err != nil {
						return err
					}
				}
				targets, err = promptAgents(io, "install", scope, projectRoot)
				if err != nil {
					return err
				}
				if len(targets) == 0 {
					return fmt.Errorf("aborted: no agents selected")
				}
				if !confirmAction(io, fmt.Sprintf("Install for %d agent(s) at %s scope?", len(targets), scope)) {
					return fmt.Errorf("aborted")
				}
			} else {
				targets, err = chooseAgents(agentSlugs, all)
				if err != nil {
					return err
				}
			}

			io.Status("installing robin SKILL.md (scope: %s)", scope)
			for _, slug := range targets {
				if err := skills.InstallTo(io.Out, slug, scope, projectRoot, force); err != nil {
					return err
				}
			}
			io.Success("done")
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&agentSlugs, "agent", nil, "agent slug to install for (repeatable). One of: "+supportedSlugs())
	cmd.Flags().BoolVar(&all, "all", false, "install for every supported agent")
	cmd.Flags().StringVar(&scope, "scope", "project", "where to install: project | user")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing SKILL.md")
	return cmd
}

func newSkillsUninstallCmd(io *IO) *cobra.Command {
	var (
		agentSlugs []string
		all        bool
		scope      string
	)
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove SKILL.md from one or more agents' skill directories.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			scopeSet := cmd.Flags().Changed("scope")
			if err := validateScope(scope); err != nil {
				return err
			}
			projectRoot, err := os.Getwd()
			if err != nil {
				return err
			}

			interactive := !io.NoInput && io.StdinTTY() &&
				len(agentSlugs) == 0 && !all
			var targets []string
			if interactive {
				if !scopeSet {
					scope, err = promptScope(io, "remove")
					if err != nil {
						return err
					}
				}
				targets, err = promptAgents(io, "remove", scope, projectRoot)
				if err != nil {
					return err
				}
				if len(targets) == 0 {
					return fmt.Errorf("aborted: no agents selected")
				}
				if !confirmAction(io, fmt.Sprintf("Remove for %d agent(s) at %s scope?", len(targets), scope)) {
					return fmt.Errorf("aborted")
				}
			} else {
				targets, err = chooseAgents(agentSlugs, all)
				if err != nil {
					return err
				}
			}

			io.Status("removing robin SKILL.md (scope: %s)", scope)
			for _, slug := range targets {
				if err := skills.UninstallFrom(io.Out, slug, scope, projectRoot); err != nil {
					return err
				}
			}
			io.Success("done")
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&agentSlugs, "agent", nil, "agent slug to remove for (repeatable). One of: "+supportedSlugs())
	cmd.Flags().BoolVar(&all, "all", false, "remove for every supported agent")
	cmd.Flags().StringVar(&scope, "scope", "project", "where to remove from: project | user")
	return cmd
}

func newSkillsListCmd(io *IO) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show the SKILL.md path (and whether it exists) for each supported agent.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateScope(scope); err != nil {
				return err
			}
			projectRoot, err := os.Getwd()
			if err != nil {
				return err
			}
			if io.JSON {
				rows := make([]map[string]any, 0, len(skills.SupportedAgents))
				for _, a := range skills.SupportedAgents {
					path, err := skills.ResolveSkillFile(a.Slug, scope, projectRoot)
					if err != nil {
						return err
					}
					rows = append(rows, map[string]any{
						"agent":     a.Slug,
						"name":      a.Name,
						"path":      path,
						"installed": fileExists(path),
					})
				}
				return io.JSONOut(rows)
			}
			tw := io.Tabular()
			fmt.Fprintln(tw, "AGENT\tINSTALLED\tPATH")
			for _, a := range skills.SupportedAgents {
				path, err := skills.ResolveSkillFile(a.Slug, scope, projectRoot)
				if err != nil {
					return err
				}
				mark := "-"
				if fileExists(path) {
					mark = "yes"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", a.Slug, mark, path)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "project", "scope to inspect: project | user")
	return cmd
}

// promptScope asks for project vs user. action is "install" / "remove" — used
// only in the prompt label so the wording reads naturally.
func promptScope(io *IO, action string) (string, error) {
	fmt.Fprintf(io.Err, "Where do you want to %s the skill?\n", action)
	fmt.Fprintln(io.Err, "  1. project — only this repo (./.<agent>/skills/robin-cli)")
	fmt.Fprintln(io.Err, "  2. user    — all your projects (~/.<agent>/skills/robin-cli)")
	r := bufio.NewReader(io.In)
	for {
		fmt.Fprint(io.Err, "Choose [1/2] (1): ")
		line, err := r.ReadString('\n')
		if err != nil {
			return "", err
		}
		switch strings.TrimSpace(line) {
		case "", "1", "project", "p":
			return "project", nil
		case "2", "user", "u":
			return "user", nil
		default:
			fmt.Fprintln(io.Err, "  invalid — type 1 or 2")
		}
	}
}

// promptAgents shows a numbered list of supported agents and accepts a
// comma-separated index list, "a"/"all", or empty (= claude-code only — the
// most common case). The "installed" column comes from inspecting the chosen
// scope so the user can see what's already in place.
func promptAgents(io *IO, action, scope, projectRoot string) ([]string, error) {
	fmt.Fprintf(io.Err, "\nWhich agents to %s for? (scope: %s)\n", action, scope)
	for i, a := range skills.SupportedAgents {
		path, err := skills.ResolveSkillFile(a.Slug, scope, projectRoot)
		if err != nil {
			return nil, err
		}
		mark := " "
		if fileExists(path) {
			mark = "✓"
		}
		fmt.Fprintf(io.Err, "  %s %d. %-11s (%s)\n", mark, i+1, a.Slug, a.Name)
	}
	r := bufio.NewReader(io.In)
	for {
		fmt.Fprint(io.Err, "Pick agents [comma-separated, 'all', blank=1]: ")
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		choice := strings.TrimSpace(line)
		if choice == "" {
			return []string{skills.SupportedAgents[0].Slug}, nil
		}
		if strings.EqualFold(choice, "a") || strings.EqualFold(choice, "all") {
			out := make([]string, 0, len(skills.SupportedAgents))
			for _, a := range skills.SupportedAgents {
				out = append(out, a.Slug)
			}
			return out, nil
		}
		picks, err := parseAgentIndices(choice)
		if err != nil {
			fmt.Fprintf(io.Err, "  invalid: %v\n", err)
			continue
		}
		return picks, nil
	}
}

// parseAgentIndices accepts "1,3,5" → []slug. Indices are 1-based and
// validated against SupportedAgents. Duplicates are deduped, original order
// preserved.
func parseAgentIndices(s string) ([]string, error) {
	parts := strings.Split(s, ",")
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("not a number: %q", p)
		}
		if n < 1 || n > len(skills.SupportedAgents) {
			return nil, fmt.Errorf("index %d out of range (1..%d)", n, len(skills.SupportedAgents))
		}
		slug := skills.SupportedAgents[n-1].Slug
		if seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, slug)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no agents selected")
	}
	return out, nil
}

func confirmAction(io *IO, msg string) bool {
	fmt.Fprintf(io.Err, "%s [Y/n] ", msg)
	r := bufio.NewReader(io.In)
	line, err := r.ReadString('\n')
	if err != nil {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "" || ans == "y" || ans == "yes"
}

func validateScope(scope string) error {
	if scope != "project" && scope != "user" {
		return fmt.Errorf("invalid --scope %q (want project or user)", scope)
	}
	return nil
}

func chooseAgents(agentSlugs []string, all bool) ([]string, error) {
	if all {
		if len(agentSlugs) > 0 {
			return nil, fmt.Errorf("pass --all OR --agent, not both")
		}
		out := make([]string, 0, len(skills.SupportedAgents))
		for _, a := range skills.SupportedAgents {
			out = append(out, a.Slug)
		}
		return out, nil
	}
	if len(agentSlugs) == 0 {
		return nil, fmt.Errorf("specify --agent (one of: %s), --all, or run interactively", supportedSlugs())
	}
	return agentSlugs, nil
}

func supportedSlugs() string {
	out := ""
	for i, a := range skills.SupportedAgents {
		if i > 0 {
			out += ", "
		}
		out += a.Slug
	}
	return out
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
