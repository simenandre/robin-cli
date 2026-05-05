package cli

import (
	"github.com/simenandre/robin-cli/internal/config"
	"github.com/simenandre/robin-cli/internal/robin"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X .../cli.Version=..."
var Version = "dev"

const (
	repoURL = "https://github.com/simenandre/robin-cli"
	docsURL = "https://github.com/simenandre/robin-cli#readme"
)

func NewRootCmd(io *IO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "robin",
		Short: "Book Robin meeting rooms from your terminal.",
		Long: `robin — book Robin meeting rooms from your terminal.

Authenticates with the Robin dashboard's email/password endpoint, so it
works without an admin-issued API token. Stores credentials and the cached
access token under your OS config directory.`,
		Example: `  # one-time setup
  robin init
  robin login

  # find and book the best priority room right now (alias: 'robin now')
  robin book

  # see what would be booked
  robin book --dry-run

  # book a room starting tomorrow at 9
  robin book --start "tomorrow 9am"

  # book a specific room
  robin book --space 172344 --start "14:00" --duration 30m

  # list bookable spaces in your location
  robin spaces`,
		Version:           Version,
		SilenceUsage:      true,
		SilenceErrors:     true,
		DisableAutoGenTag: true,
	}
	cmd.SetVersionTemplate("robin {{.Version}}\n")

	cmd.PersistentFlags().BoolVar(&io.JSON, "json", false, "output as JSON")
	cmd.PersistentFlags().BoolVarP(&io.Quiet, "quiet", "q", false, "suppress non-essential output")
	cmd.PersistentFlags().BoolVarP(&io.Verbose, "verbose", "v", false, "show extra detail (HTTP exchange, per-room availability)")
	cmd.PersistentFlags().BoolVar(&io.NoColor, "no-color", false, "disable color output")
	cmd.PersistentFlags().BoolVar(&io.NoInput, "no-input", false, "fail rather than prompt for input")

	cmd.AddCommand(
		newInitCmd(io),
		newLoginCmd(io),
		newLogoutCmd(io),
		newWhoamiCmd(io),
		newOrgsCmd(io),
		newLocationsCmd(io),
		newSpacesCmd(io),
		newBookCmd(io),
		newPriorityCmd(io),
		newSkillsCmd(io),
	)

	cmd.SetHelpTemplate(`{{with .Long}}{{. | trimTrailingWhitespaces}}{{else}}{{.Short}}{{end}}

Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}

Learn more at ` + docsURL + `
Report issues at ` + repoURL + `/issues
`)

	cobra.OnInitialize(func() { io.applyFlags() })
	return cmd
}

// Execute runs the CLI with the given args. Returns the exit code.
func Execute() int {
	io := NewIO()
	err := NewRootCmd(io).Execute()
	if err == nil {
		return 0
	}
	if isSessionError(err) {
		if relogErr := tryRelogin(io); relogErr == nil {
			if err2 := NewRootCmd(io).Execute(); err2 == nil {
				return 0
			} else {
				err = err2
			}
		}
	}
	io.Errorf("%s", err)
	hintForError(io, err)
	return 1
}

// tryRelogin reads stored credentials and refreshes the cached session.
func tryRelogin(io *IO) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	c := robin.New()
	c.Verbose = io.Verbose
	sess, err := c.Login(cfg.Email, cfg.Password, nil)
	if err != nil {
		return err
	}
	if err := robin.SaveSession(sess); err != nil {
		return err
	}
	io.Status("session refreshed")
	return nil
}

func isSessionError(err error) bool {
	msg := err.Error()
	return contains(msg, "401") ||
		contains(msg, "expired") ||
		contains(msg, "no session") ||
		contains(msg, "not authenticated")
}

// hintForError adds a recovery suggestion based on what we know about the
// error. clig.dev: errors should guide users toward a fix.
func hintForError(io *IO, err error) {
	msg := err.Error()
	switch {
	case contains(msg, "no config"), contains(msg, "missing org"):
		io.Status("hint: run %s to save your credentials.", boldCmd("robin init"))
	case contains(msg, "401"), contains(msg, "expired"),
		contains(msg, "no session"), contains(msg, "not authenticated"):
		io.Status("hint: run %s to authenticate.", boldCmd("robin login"))
	case contains(msg, "quick_book"):
		io.Status("hint: see %s for required quick_book fields.", boldCmd("robin now --help"))
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
