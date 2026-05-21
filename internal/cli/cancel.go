package cli

import (
	"fmt"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/simenandre/robin-cli/internal/robin"
	"github.com/spf13/cobra"
)

func newCancelCmd(io *IO) *cobra.Command {
	var (
		eventID  string
		all      bool
		multiple bool
		window   time.Duration
		yes      bool
		dryRun   bool
	)
	cmd := &cobra.Command{
		Use:   "cancel",
		Short: "Cancel one of your upcoming bookings.",
		Long: `List upcoming events you've booked and delete the one(s) you pick.

With no flags on an interactive terminal, robin shows a fuzzy-filterable
single-select of your upcoming events. Pass --multiple to pick several
at once (space toggles, no fuzzy search). Pass --event <id> to cancel
one specific event, or --all to cancel every booking in the window.`,
		Example: `  # interactive single-select (with fuzzy filter)
  robin cancel

  # multi-select picker (space to toggle)
  robin cancel --multiple

  # cancel a specific event
  robin cancel --event abc123

  # cancel everything in the next 7 days, no prompt
  robin cancel --all --yes

  # preview what --all would cancel
  robin cancel --all --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if eventID != "" && all {
				return fmt.Errorf("pass --event OR --all, not both")
			}
			c, err := authedClient(io)
			if err != nil {
				return err
			}

			if eventID != "" {
				return cancelOne(io, c, eventID, yes, dryRun)
			}

			now := time.Now()
			events, err := c.MyEvents(now, now.Add(window))
			if err != nil {
				return err
			}
			var upcoming []robin.Event
			for _, ev := range events {
				end, _ := ev.End.Time()
				if end.After(now) {
					upcoming = append(upcoming, ev)
				}
			}
			if len(upcoming) == 0 {
				io.Status("no upcoming events in the next %v", window)
				return nil
			}

			var targets []robin.Event
			switch {
			case all:
				targets = upcoming
			case io.NoInput || !io.StdinTTY():
				return fmt.Errorf("specify --event <id>, --all, or --multiple (no terminal for interactive picker)")
			case multiple:
				targets, err = pickEventsToCancel(upcoming)
				if err != nil {
					return err
				}
				if len(targets) == 0 {
					return fmt.Errorf("aborted: no events selected")
				}
			default:
				pick, err := pickEventToCancel(upcoming)
				if err != nil {
					return err
				}
				if pick.ID == "" {
					return fmt.Errorf("aborted: nothing selected")
				}
				targets = []robin.Event{pick}
			}

			if !yes && !dryRun {
				if io.NoInput || !io.StdinTTY() {
					return fmt.Errorf("refusing to cancel %d event(s) without --yes (no terminal)", len(targets))
				}
				ok := false
				if err := huh.NewConfirm().
					Title(fmt.Sprintf("Cancel %d event(s)?", len(targets))).
					Description(summarizeEvents(targets)).
					Affirmative("Yes, cancel").
					Negative("Keep them").
					Value(&ok).
					Run(); err != nil {
					return fmt.Errorf("cancel: %w", err)
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			}

			return cancelMany(io, c, targets, dryRun)
		},
	}
	cmd.Flags().StringVar(&eventID, "event", "", "cancel one specific event by ID")
	cmd.Flags().BoolVar(&all, "all", false, "cancel every upcoming event in the window")
	cmd.Flags().BoolVarP(&multiple, "multiple", "m", false, "open a multi-select picker instead of single-select")
	cmd.Flags().DurationVar(&window, "window", 7*24*time.Hour, "look this far ahead for cancellable events")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "show what would be cancelled without calling Robin")
	return cmd
}

func cancelOne(io *IO, c *robin.Client, eventID string, yes, dryRun bool) error {
	label := "event " + eventID
	if !yes && !dryRun {
		if io.NoInput || !io.StdinTTY() {
			return fmt.Errorf("refusing to cancel %s without --yes (no terminal)", label)
		}
		ok := false
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Cancel %s?", label)).
			Affirmative("Yes, cancel").
			Negative("Keep it").
			Value(&ok).
			Run(); err != nil {
			return fmt.Errorf("cancel: %w", err)
		}
		if !ok {
			return fmt.Errorf("aborted")
		}
	}
	if dryRun {
		if io.JSON {
			return io.JSONOut(map[string]any{"dry_run": true, "event_id": eventID})
		}
		io.Success("would cancel %s", label)
		return nil
	}
	if err := c.DeleteEvent(eventID); err != nil {
		return err
	}
	if io.JSON {
		return io.JSONOut(map[string]any{"event_id": eventID, "status": "cancelled"})
	}
	io.Success("cancelled %s", label)
	return nil
}

func cancelMany(io *IO, c *robin.Client, events []robin.Event, dryRun bool) error {
	type result struct {
		EventID string `json:"event_id"`
		Title   string `json:"title,omitempty"`
		Start   string `json:"start"`
		End     string `json:"end"`
		Status  string `json:"status"`
		Error   string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(events))
	var cancelled, failed int
	for _, ev := range events {
		start, _ := ev.Start.Time()
		end, _ := ev.End.Time()
		r := result{
			EventID: ev.ID,
			Title:   ev.Title,
			Start:   start.Format(time.RFC3339),
			End:     end.Format(time.RFC3339),
		}
		label := titleOrPlaceholder(ev.Title)
		when := start.Local().Format("Mon Jan 2 15:04") + "–" + end.Local().Format("15:04")
		if dryRun {
			r.Status = "dry_run"
			io.Success("would cancel: %s (%s)", label, when)
		} else if err := c.DeleteEvent(ev.ID); err != nil {
			r.Status = "error"
			r.Error = err.Error()
			failed++
			io.Status("failed: %s (%s) — %v", label, when, err)
		} else {
			r.Status = "cancelled"
			cancelled++
			io.Success("cancelled: %s (%s)", label, when)
		}
		results = append(results, r)
	}
	if io.JSON {
		return io.JSONOut(map[string]any{
			"cancelled_count": cancelled,
			"failed_count":    failed,
			"results":         results,
		})
	}
	if dryRun {
		io.Status("dry run: %d would be cancelled", len(events))
	} else {
		io.Status("done: %d cancelled, %d failed", cancelled, failed)
	}
	return nil
}

func pickEventToCancel(events []robin.Event) (robin.Event, error) {
	options := make([]huh.Option[string], 0, len(events))
	byID := make(map[string]robin.Event, len(events))
	for _, ev := range events {
		options = append(options, huh.NewOption(cancelOptionLabel(ev), ev.ID))
		byID[ev.ID] = ev
	}
	var picked string
	if err := huh.NewSelect[string]().
		Title("Pick an event to cancel").
		Description("Type / to filter.").
		Filtering(true).
		Options(options...).
		Value(&picked).
		Run(); err != nil {
		return robin.Event{}, fmt.Errorf("cancel: %w", err)
	}
	return byID[picked], nil
}

func pickEventsToCancel(events []robin.Event) ([]robin.Event, error) {
	options := make([]huh.Option[string], 0, len(events))
	byID := make(map[string]robin.Event, len(events))
	for _, ev := range events {
		options = append(options, huh.NewOption(cancelOptionLabel(ev), ev.ID))
		byID[ev.ID] = ev
	}
	var picked []string
	if err := huh.NewMultiSelect[string]().
		Title("Pick events to cancel").
		Description("Space to toggle, enter to confirm.").
		Options(options...).
		Value(&picked).
		Run(); err != nil {
		return nil, fmt.Errorf("cancel: %w", err)
	}
	out := make([]robin.Event, 0, len(picked))
	for _, id := range picked {
		out = append(out, byID[id])
	}
	return out, nil
}

func cancelOptionLabel(ev robin.Event) string {
	start, _ := ev.Start.Time()
	end, _ := ev.End.Time()
	return fmt.Sprintf("%s — %s–%s",
		titleOrPlaceholder(ev.Title),
		start.Local().Format("Mon Jan 2 15:04"),
		end.Local().Format("15:04"))
}

func summarizeEvents(events []robin.Event) string {
	if len(events) == 0 {
		return ""
	}
	lines := make([]string, 0, len(events))
	for _, ev := range events {
		start, _ := ev.Start.Time()
		end, _ := ev.End.Time()
		lines = append(lines, fmt.Sprintf("  • %s — %s–%s",
			titleOrPlaceholder(ev.Title),
			start.Local().Format("Mon Jan 2 15:04"),
			end.Local().Format("15:04")))
	}
	return joinLines(lines)
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
