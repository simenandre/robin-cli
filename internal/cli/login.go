package cli

import (
	"github.com/simenandre/robin-cli/internal/config"
	"github.com/simenandre/robin-cli/internal/robin"
	"github.com/spf13/cobra"
)

func newLoginCmd(io *IO) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate with Robin and cache an access token.",
		Long: `Reads credentials from the config file and exchanges them for an
access token via the dashboard's auth endpoint. The token is cached at:

  ` + sessionFilePath() + `

Subsequent commands use the cached token until it expires.`,
		Example: `  robin login`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			if io.JSON {
				return io.JSONOut(map[string]any{
					"account_id":  sess.AccountID,
					"expires_at":  sess.ExpiresAt,
					"saved_at":    sess.SavedAt,
				})
			}
			io.Success("logged in (account %d)", sess.AccountID)
			return nil
		},
	}
}

func sessionFilePath() string {
	dir, err := config.Dir()
	if err != nil {
		return "<config dir>"
	}
	return dir + "/session.json"
}
