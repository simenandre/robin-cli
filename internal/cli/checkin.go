package cli

import (
	"time"

	"github.com/spf13/cobra"
)

func newCheckInCmd(io *IO) *cobra.Command {
	var (
		lookahead time.Duration
		dryRun    bool
	)
	cmd := &cobra.Command{
		Use:     "check-in",
		Aliases: []string{"checkin"},
		Short:   "Check in to all your bookings within Robin's confirmation window.",
		Long: `Look up events you have booked between (now - 15m) and
(now + --lookahead), then call Robin's confirmation endpoint for each
that hasn't been checked in yet.

Robin's check-in window is server-side; events outside it (too early,
too late) come back as a 4xx error and are reported but skipped.`,
		Example: `  robin check-in
  robin check-in --lookahead 1h
  robin check-in --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := authedClient(io)
			if err != nil {
				return err
			}
			now := time.Now()
			after := now.Add(-15 * time.Minute)
			before := now.Add(lookahead)

			events, err := c.MyEvents(after, before)
			if err != nil {
				return err
			}

			type result struct {
				EventID string `json:"event_id"`
				Title   string `json:"title,omitempty"`
				SpaceID int64  `json:"space_id,omitempty"`
				Start   string `json:"start"`
				End     string `json:"end"`
				Status  string `json:"status"` // checked_in | already | dry_run | error | ended
				Error   string `json:"error,omitempty"`
			}

			results := make([]result, 0, len(events))
			var checkedIn, skipped int

			for _, ev := range events {
				start, _ := ev.Start.Time()
				end, _ := ev.End.Time()
				r := result{
					EventID: ev.ID,
					Title:   ev.Title,
					SpaceID: ev.SpaceID,
					Start:   start.Format(time.RFC3339),
					End:     end.Format(time.RFC3339),
				}
				label := titleOrPlaceholder(ev.Title)
				when := start.Local().Format("15:04") + "–" + end.Local().Format("15:04")

				switch {
				case !end.After(now):
					r.Status = "ended"
					skipped++
					io.Status("skip (ended): %s (%s)", label, when)
				case ev.IsConfirmed():
					r.Status = "already"
					skipped++
					io.Status("skip (already checked in): %s (%s)", label, when)
				case dryRun:
					r.Status = "dry_run"
					io.Success("would check in: %s (%s)", label, when)
				default:
					if err := c.ConfirmEvent(ev.ID); err != nil {
						r.Status = "error"
						r.Error = err.Error()
						skipped++
						io.Status("skip (error): %s (%s) — %v", label, when, err)
					} else {
						r.Status = "checked_in"
						checkedIn++
						io.Success("checked in: %s (%s)", label, when)
					}
				}
				results = append(results, r)
			}

			if io.JSON {
				return io.JSONOut(map[string]any{
					"checked_in_count": checkedIn,
					"skipped_count":    skipped,
					"results":          results,
				})
			}

			if len(events) == 0 {
				io.Status("no events in the next %v", lookahead)
				return nil
			}
			if dryRun {
				io.Status("dry run: %d would be checked in, %d skipped", len(results)-skipped, skipped)
			} else {
				io.Status("done: %d checked in, %d skipped", checkedIn, skipped)
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&lookahead, "lookahead", 30*time.Minute, "look at events starting up to this far in the future")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "show what would be checked in without calling Robin")
	return cmd
}

func titleOrPlaceholder(s string) string {
	if s == "" {
		return "(untitled)"
	}
	return s
}
