package cli

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

func newOrgsCmd(io *IO) *cobra.Command {
	return &cobra.Command{
		Use:     "orgs",
		Aliases: []string{"organizations"},
		Short:   "List Robin organizations you belong to.",
		Example: `  robin orgs
  robin orgs --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := authedClient(io)
			if err != nil {
				return err
			}
			orgs, err := c.MyOrganizations()
			if err != nil {
				return err
			}
			if io.JSON {
				return io.JSONOut(orgs)
			}
			tw := io.Tabular()
			fmt.Fprintln(tw, header("ID\tSLUG\tNAME"))
			for _, o := range orgs {
				fmt.Fprintf(tw, "%d\t%s\t%s\n", o.ID, o.Slug, o.Name)
			}
			return tw.Flush()
		},
	}
}

func header(s string) string {
	if color.NoColor {
		return s
	}
	return color.New(color.Bold).Sprint(s)
}
