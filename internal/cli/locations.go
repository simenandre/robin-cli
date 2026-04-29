package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newLocationsCmd(io *IO) *cobra.Command {
	return &cobra.Command{
		Use:     "locations",
		Aliases: []string{"locs"},
		Short:   "List locations across your organizations.",
		Example: `  robin locations
  robin locations --json`,
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
			type row struct {
				LocationID   int64  `json:"location_id"`
				LocationName string `json:"location_name"`
				OrgSlug      string `json:"org_slug"`
				OrgID        int64  `json:"org_id"`
			}
			var rows []row
			for _, o := range orgs {
				locs, err := c.Locations(o.ID)
				if err != nil {
					return fmt.Errorf("locations for %s: %w", o.Slug, err)
				}
				for _, l := range locs {
					rows = append(rows, row{l.ID, l.Name, o.Slug, o.ID})
				}
			}
			if io.JSON {
				return io.JSONOut(rows)
			}
			tw := io.Tabular()
			fmt.Fprintln(tw, header("ID\tNAME\tORG"))
			for _, r := range rows {
				fmt.Fprintf(tw, "%d\t%s\t%s\n", r.LocationID, r.LocationName, r.OrgSlug)
			}
			return tw.Flush()
		},
	}
}
