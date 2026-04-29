package cli

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"github.com/simenandre/robin-user-api/internal/robin"
	"github.com/spf13/cobra"
)

func newBookCmd(io *IO) *cobra.Command {
	var (
		spaceID     int64
		startStr    string
		endStr      string
		durationStr string
		title       string
		description string
		tzName      string
		yes         bool
	)

	cmd := &cobra.Command{
		Use:   "book",
		Short: "Book a specific space.",
		Long: `Creates an event in the given space. Provide either --end or --duration
(not both). Times accept RFC3339 (2026-04-29T09:00:00+02:00) or local
forms (2026-04-29 09:00, 2026-04-29T09:00:00).

Robin requires events to be at least 5 minutes long.`,
		Example: `  robin book --space 172344 --start "2026-04-29 14:00" --duration 30m
  robin book --space 172344 --start "2026-04-29T14:00:00+02:00" --end "2026-04-29T15:00:00+02:00" --title "Sync"
  robin book --space 172344 --start "14:00" --duration 1h --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if spaceID == 0 || startStr == "" {
				return fmt.Errorf("--space and --start are required")
			}
			if (endStr == "") == (durationStr == "") {
				return fmt.Errorf("provide exactly one of --end or --duration")
			}
			tz, err := time.LoadLocation(tzName)
			if err != nil {
				return fmt.Errorf("invalid --time-zone: %w", err)
			}
			now := time.Now().In(tz)
			start, err := parseWhenOrTime(startStr, now, tz)
			if err != nil {
				return fmt.Errorf("invalid --start: %w", err)
			}
			var end time.Time
			if endStr != "" {
				end, err = parseWhenOrTime(endStr, now, tz)
				if err != nil {
					return fmt.Errorf("invalid --end: %w", err)
				}
			} else {
				dur, err := time.ParseDuration(durationStr)
				if err != nil {
					return fmt.Errorf("invalid --duration: %w", err)
				}
				end = start.Add(dur)
			}
			if !end.After(start) {
				return fmt.Errorf("--end must be after --start")
			}
			if end.Sub(start) < 5*time.Minute {
				return fmt.Errorf("Robin requires events to be at least 5 minutes")
			}

			sLocal := start.In(tz)
			eLocal := end.In(tz)
			io.Status("Booking %s — %s to %s (%v)",
				faintInt(spaceID), sLocal.Format("Mon Jan 2 15:04"),
				eLocal.Format("15:04"), eLocal.Sub(sLocal))

			if !yes && io.StdinTTY() && !io.NoInput {
				if !confirm(io, "Proceed?") {
					return fmt.Errorf("aborted")
				}
			} else if !yes && (io.NoInput || !io.StdinTTY()) {
				return fmt.Errorf("refusing to book without --yes (no interactive terminal)")
			}

			c, err := authedClient(io)
			if err != nil {
				return err
			}
			req := robin.BookRequest{
				Title:       title,
				Description: description,
				Start:       robin.DateTime{DateTime: sLocal.Format(time.RFC3339), TimeZone: tzName},
				End:         robin.DateTime{DateTime: eLocal.Format(time.RFC3339), TimeZone: tzName},
			}
			ev, err := c.BookSpace(spaceID, req)
			if err != nil {
				return err
			}
			if io.JSON {
				return io.JSONOut(map[string]any{
					"event_id":         ev.ID,
					"space_id":         spaceID,
					"start":            sLocal.Format(time.RFC3339),
					"end":              eLocal.Format(time.RFC3339),
					"duration_minutes": int(eLocal.Sub(sLocal) / time.Minute),
				})
			}
			io.Success("booked event %s", ev.ID)
			return nil
		},
	}

	cmd.Flags().Int64VarP(&spaceID, "space", "s", 0, "space ID to book (required)")
	cmd.Flags().StringVar(&startStr, "start", "", "start time")
	cmd.Flags().StringVar(&endStr, "end", "", "end time (mutually exclusive with --duration)")
	cmd.Flags().StringVarP(&durationStr, "duration", "d", "", "duration like 30m, 1h (mutually exclusive with --end)")
	cmd.Flags().StringVar(&title, "title", "", "event title (Robin auto-generates one if empty)")
	cmd.Flags().StringVar(&description, "description", "", "event description")
	cmd.Flags().StringVar(&tzName, "time-zone", "Europe/Oslo", "IANA timezone for start/end")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	_ = cmd.MarkFlagRequired("space")
	_ = cmd.MarkFlagRequired("start")
	return cmd
}

func confirm(io *IO, msg string) bool {
	fmt.Fprintf(io.Err, "%s [y/N] ", msg)
	r := bufio.NewReader(io.In)
	line, err := r.ReadString('\n')
	if err != nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}
