package cli

import (
	"fmt"

	"github.com/simenandre/robin-cli/internal/config"
	"github.com/spf13/cobra"
)

func newPriorityCmd(io *IO) *cobra.Command {
	return &cobra.Command{
		Use:   "priority",
		Short: "Edit the auto-pick priority list of meeting rooms.",
		Long: `Walk through the spaces in your configured quick_book.location
and pick (in order) which rooms 'robin book' should prefer when more
than one is free.

Requires that you've run 'robin init' (to set the location) and
'robin login' (so the CLI can list spaces). Only rooms named
'Meeting Room <N>' are pickable — that's the convention robin's
auto-pick code uses to map priority numbers to spaces.`,
		Example: `  robin priority`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if io.NoInput {
				return fmt.Errorf("priority requires interactive input; cannot run with --no-input")
			}
			if !io.StdinTTY() {
				return fmt.Errorf("priority requires a terminal")
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.QuickBook == nil || cfg.QuickBook.Location == 0 {
				return fmt.Errorf("quick_book.location is not set; run %s first", boldCmd("robin init"))
			}
			c, err := authedClient(io)
			if err != nil {
				return err
			}
			updated, err := pickPriorityRooms(c, cfg.QuickBook.Location, cfg.QuickBook.Priority)
			if err != nil {
				return err
			}
			cfg.QuickBook.Priority = updated
			if err := config.Save(cfg); err != nil {
				return err
			}
			io.Success("priority list saved (%d rooms)", len(updated))
			return nil
		},
	}
}
