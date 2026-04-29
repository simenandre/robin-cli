package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/simenandre/robin-cli/internal/config"
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
this is the trade-off for accessing Robin without an admin API token.`,
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
			c := &config.Config{Org: org, Email: email, Password: string(pw)}
			if err := config.Save(c); err != nil {
				return err
			}
			io.Success("saved %s", configFilePath())
			io.Status("next: run %s", boldCmd("robin login"))
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
