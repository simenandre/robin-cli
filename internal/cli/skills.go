package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
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
into its context. Skills always install at the user level (under your
home directory) so they're available to every project.

Run 'robin skills install' with no flags on an interactive terminal to
pick which agents to install for.`,
		Example: `  # Interactive — pick agents
  robin skills install

  # Install for Claude Code
  robin skills install --agent claude-code

  # Install for several agents at once
  robin skills install --agent claude-code --agent codex

  # Install for all supported agents
  robin skills install --all

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
		force      bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write SKILL.md into one or more agents' skill directories.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			interactive := !io.NoInput && io.StdinTTY() &&
				len(agentSlugs) == 0 && !all
			var (
				targets []string
				err     error
			)
			if interactive {
				targets, err = promptAgents("install")
				if err != nil {
					return err
				}
				if len(targets) == 0 {
					return fmt.Errorf("aborted: no agents selected")
				}
				ok, err := confirmAction(fmt.Sprintf("Install for %d agent(s)?", len(targets)))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			} else {
				targets, err = chooseAgents(agentSlugs, all)
				if err != nil {
					return err
				}
			}

			io.Status("installing robin SKILL.md")
			for _, slug := range targets {
				if err := skills.InstallTo(io.Out, slug, force); err != nil {
					return err
				}
			}
			io.Success("done")
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&agentSlugs, "agent", nil, "agent slug to install for (repeatable). One of: "+supportedSlugs())
	cmd.Flags().BoolVar(&all, "all", false, "install for every supported agent")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing SKILL.md")
	return cmd
}

func newSkillsUninstallCmd(io *IO) *cobra.Command {
	var (
		agentSlugs []string
		all        bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove SKILL.md from one or more agents' skill directories.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			interactive := !io.NoInput && io.StdinTTY() &&
				len(agentSlugs) == 0 && !all
			var (
				targets []string
				err     error
			)
			if interactive {
				targets, err = promptAgents("remove")
				if err != nil {
					return err
				}
				if len(targets) == 0 {
					return fmt.Errorf("aborted: no agents selected")
				}
				ok, err := confirmAction(fmt.Sprintf("Remove for %d agent(s)?", len(targets)))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			} else {
				targets, err = chooseAgents(agentSlugs, all)
				if err != nil {
					return err
				}
			}

			io.Status("removing robin SKILL.md")
			for _, slug := range targets {
				if err := skills.UninstallFrom(io.Out, slug); err != nil {
					return err
				}
			}
			io.Success("done")
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&agentSlugs, "agent", nil, "agent slug to remove for (repeatable). One of: "+supportedSlugs())
	cmd.Flags().BoolVar(&all, "all", false, "remove for every supported agent")
	return cmd
}

func newSkillsListCmd(io *IO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show the SKILL.md path (and whether it exists) for each supported agent.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if io.JSON {
				rows := make([]map[string]any, 0, len(skills.SupportedAgents))
				for _, a := range skills.SupportedAgents {
					path, err := skills.ResolveSkillFile(a.Slug)
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
				path, err := skills.ResolveSkillFile(a.Slug)
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
	return cmd
}

// promptAgents shows a multi-select with an "(installed)" hint next to agents
// already on disk. Defaults to Claude Code if nothing is installed yet;
// otherwise pre-selects whatever is already there.
func promptAgents(action string) ([]string, error) {
	options := make([]huh.Option[string], 0, len(skills.SupportedAgents))
	defaults := []string{}
	for _, a := range skills.SupportedAgents {
		path, err := skills.ResolveSkillFile(a.Slug)
		if err != nil {
			return nil, err
		}
		label := a.Name
		if fileExists(path) {
			label += "  (installed)"
			defaults = append(defaults, a.Slug)
		}
		options = append(options, huh.NewOption(label, a.Slug))
	}
	if len(defaults) == 0 {
		defaults = []string{"claude-code"}
	}

	picked := append([]string(nil), defaults...)
	err := huh.NewMultiSelect[string]().
		Title(fmt.Sprintf("Which agents should robin %s the skill for?", action)).
		Description("Use space to toggle, enter to confirm.").
		Options(options...).
		Value(&picked).
		Run()
	if err != nil {
		return nil, fmt.Errorf("skills: %w", err)
	}
	return picked, nil
}

func confirmAction(msg string) (bool, error) {
	ok := true
	err := huh.NewConfirm().
		Title(msg).
		Affirmative("Yes").
		Negative("Cancel").
		Value(&ok).
		Run()
	if err != nil {
		return false, fmt.Errorf("skills: %w", err)
	}
	return ok, nil
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
