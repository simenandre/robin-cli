package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/charmbracelet/huh"
	"github.com/simenandre/robin-cli/internal/config"
	"github.com/simenandre/robin-cli/internal/robin"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newInitCmd(io *IO) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Save Robin credentials to a config file.",
		Long: `Prompts for org slug, email, and password, then writes them to:

  ` + configFilePath() + `

The file is created with mode 0600. Credentials are stored in plaintext;
this is the trade-off for accessing Robin without an admin API token.

After saving credentials, init offers to set up quick_book (default
location, priority rooms, time zone, durations) in the same flow.`,
		Example: `  robin init`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if io.NoInput {
				return fmt.Errorf("init requires interactive input; cannot run with --no-input")
			}
			if !io.StdinTTY() {
				return fmt.Errorf("init requires a terminal for password entry")
			}

			in := bufio.NewReader(os.Stdin)
			org, err := readLine(in, "org slug (e.g. startuplab): ")
			if err != nil {
				return err
			}
			email, err := readLine(in, "email: ")
			if err != nil {
				return err
			}
			fmt.Fprint(io.Err, "password: ")
			pw, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Fprintln(io.Err)
			if err != nil {
				return err
			}
			cfg := &config.Config{Org: org, Email: email, Password: string(pw)}
			if err := config.Save(cfg); err != nil {
				return err
			}
			io.Success("saved %s", configFilePath())

			setUp := false
			if err := huh.NewConfirm().
				Title("Set up quick_book now?").
				Description("Pick default location, priority rooms, time zone, and durations.").
				Affirmative("Yes").
				Negative("Skip").
				Value(&setUp).
				Run(); err != nil {
				return fmt.Errorf("init: %w", err)
			}
			if !setUp {
				io.Status("next: run %s, then %s", boldCmd("robin login"), boldCmd("robin priority"))
				return nil
			}

			io.Status("logging in to discover orgs / locations / spaces...")
			c := robin.New()
			c.Verbose = io.Verbose
			sess, err := c.Login(cfg.Email, cfg.Password, nil)
			if err != nil {
				return fmt.Errorf("login (so we can list spaces): %w", err)
			}
			if err := robin.SaveSession(sess); err != nil {
				return err
			}

			qb, err := runQuickBookSetup(c, cfg.QuickBook)
			if err != nil {
				return err
			}
			cfg.QuickBook = qb
			if err := config.Save(cfg); err != nil {
				return err
			}
			io.Success("quick_book saved (%d priority rooms, location %d)", len(qb.Priority), qb.Location)
			return nil
		},
	}
}

func readLine(r *bufio.Reader, label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(line)
	if s == "" {
		return "", fmt.Errorf("empty value")
	}
	return s, nil
}

func configFilePath() string {
	dir, err := config.Dir()
	if err != nil {
		return "<config dir>"
	}
	return dir + "/config.json"
}
