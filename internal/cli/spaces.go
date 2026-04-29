package cli

import (
	"fmt"

	"github.com/simenandre/robin-cli/internal/config"
	"github.com/simenandre/robin-cli/internal/robin"
	"github.com/spf13/cobra"
)

func newSpacesCmd(io *IO) *cobra.Command {
	var locationID int64
	var includeDisabled bool

	cmd := &cobra.Command{
		Use:     "spaces",
		Aliases: []string{"rooms"},
		Short:   "List bookable spaces.",
		Long: `Lists bookable spaces. By default, lists every space across every
location in every organization. Pass --location to scope.

If quick_book.location is set in your config and --location is not given,
that location is used.`,
		Example: `  robin spaces
  robin spaces --location 22847
  robin spaces --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := authedClient(io)
			if err != nil {
				return err
			}
			cfg, _ := config.Load()
			if locationID == 0 && cfg != nil && cfg.QuickBook != nil {
				locationID = cfg.QuickBook.Location
			}

			var locIDs []int64
			if locationID != 0 {
				locIDs = []int64{locationID}
			} else {
				orgs, err := c.MyOrganizations()
				if err != nil {
					return err
				}
				for _, o := range orgs {
					locs, err := c.Locations(o.ID)
					if err != nil {
						return err
					}
					for _, l := range locs {
						locIDs = append(locIDs, l.ID)
					}
				}
			}

			var all []robin.Space
			for _, id := range locIDs {
				spaces, err := c.Spaces(id)
				if err != nil {
					return fmt.Errorf("spaces for location %d: %w", id, err)
				}
				for _, s := range spaces {
					if !includeDisabled && s.IsDisabled {
						continue
					}
					all = append(all, s)
				}
			}
			if io.JSON {
				return io.JSONOut(all)
			}
			tw := io.Tabular()
			fmt.Fprintln(tw, header("ID\tLOCATION\tCAPACITY\tTYPE\tNAME"))
			for _, s := range all {
				fmt.Fprintf(tw, "%d\t%d\t%d\t%s\t%s\n", s.ID, s.LocationID, s.Capacity, s.Type, s.Name)
			}
			return tw.Flush()
		},
	}

	cmd.Flags().Int64VarP(&locationID, "location", "l", 0, "filter to a single location ID")
	cmd.Flags().BoolVar(&includeDisabled, "all", false, "include disabled spaces")
	return cmd
}
