package cli

import (
	"errors"
	"os"

	"github.com/spf13/cobra"
)

func newLogoutCmd(io *IO) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete the cached access token.",
		Long: `Removes ` + sessionFilePath() + `.

Does not touch the config file (credentials).`,
		Example: `  robin logout`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := sessionFilePath()
			err := os.Remove(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			io.Success("session cleared")
			return nil
		},
	}
}
