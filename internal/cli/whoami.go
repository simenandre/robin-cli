package cli

import (
	"github.com/spf13/cobra"
)

func newWhoamiCmd(io *IO) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Print the currently authenticated user.",
		Example: `  robin whoami
  robin whoami --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := authedClient(io)
			if err != nil {
				return err
			}
			me, err := c.Whoami()
			if err != nil {
				return err
			}
			if io.JSON {
				return io.JSONOut(me)
			}
			io.Printf("%s <%s>\n  account id:  %d\n  user slug:   %s\n",
				me.Name, me.PrimaryEmail.Email, me.ID, me.Slug)
			return nil
		},
	}
}
