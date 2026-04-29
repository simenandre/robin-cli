package cli

import (
	"bufio"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/simenandre/robin-user-api/internal/config"
	"github.com/simenandre/robin-user-api/internal/robin"
	"github.com/spf13/cobra"
)

func newBookCmd(io *IO) *cobra.Command {
	var (
		spaceID          int64
		startStr         string
		endStr           string
		durationStr      string
		title            string
		description      string
		tzName           string
		yes              bool
		dryRun           bool
		minMin           int
		maxMin           int
		windowMin        int
		prioritizeLength bool
	)

	cmd := &cobra.Command{
		Use:     "book",
		Aliases: []string{"now"},
		Short:   "Book a meeting room.",
		Long: `Book a meeting room.

Two modes, picked by whether you supply --space:

  Auto-pick (default): finds the best available room based on your
                       quick_book config. --start is the earliest
                       acceptable start; the picker takes the longest
                       free slot up to --max.

  Specific (--space):  books that exact space. --start is the exact
                       start time. Provide --duration or --end.

--start, --end, and --duration accept natural language: "tomorrow 9am",
"in 2 hours", "monday 9am" — plus durations ("1h", "30m"), clock times
("14:00"), and full datetimes ("2026-04-29 09:00", RFC3339).`,
		Example: `  # auto-pick (the common case — also available as 'robin now')
  robin book                                # best room, starting now
  robin book --start "tomorrow 9am"         # best room tomorrow at 9
  robin book --start now --duration 1h      # exactly 1h, starting now
  robin book --dry-run                      # see the pick without booking
  robin book --prioritize-length            # take the longest slot anywhere
  robin book --max 60                       # cap at 60 min

  # specific space
  robin book --space 172344 --start "tomorrow 9am" --duration 30m
  robin book --space 172344 --start "14:00" --duration 1h --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tz, tzNameResolved, err := resolveTZ(tzName)
			if err != nil {
				return err
			}
			if spaceID != 0 {
				return runSpecificBook(io, runSpecificBookArgs{
					spaceID: spaceID, startStr: startStr, endStr: endStr,
					durationStr: durationStr, title: title, description: description,
					tz: tz, tzName: tzNameResolved, yes: yes, dryRun: dryRun,
				})
			}
			if endStr != "" {
				return fmt.Errorf("--end is only valid with --space; use --duration in auto-pick mode")
			}
			return runAutoPickBook(io, runAutoPickArgs{
				startStr: startStr, durationStr: durationStr, title: title,
				description: description, tz: tz, tzName: tzNameResolved,
				minMin: minMin, maxMin: maxMin, windowMin: windowMin,
				prioritizeLength: prioritizeLength, dryRun: dryRun,
			})
		},
	}

	// Identifying / target flags
	cmd.Flags().Int64VarP(&spaceID, "space", "s", 0, "specific space ID (skip auto-pick)")

	// When / how long
	cmd.Flags().StringVar(&startStr, "start", "", "start time (auto-pick: earliest start; specific: exact). Default: now")
	cmd.Flags().StringVar(&endStr, "end", "", "end time (specific mode only)")
	cmd.Flags().StringVarP(&durationStr, "duration", "d", "", "fixed length, e.g. 30m / 1h. Required for --space")

	// Auto-pick controls
	cmd.Flags().IntVar(&minMin, "min", 0, "auto-pick: minimum slot length in minutes")
	cmd.Flags().IntVar(&maxMin, "max", 0, "auto-pick: maximum booking length in minutes")
	cmd.Flags().IntVar(&windowMin, "window", 0, "auto-pick: search window past --start in minutes")
	cmd.Flags().BoolVarP(&prioritizeLength, "prioritize-length", "L", false, "auto-pick: rank by length, ignore priority order")

	// Event content + behavior
	cmd.Flags().StringVar(&title, "title", "", "event title (Robin auto-generates one if empty)")
	cmd.Flags().StringVar(&description, "description", "", "event description")
	cmd.Flags().StringVar(&tzName, "time-zone", "", "IANA timezone (default: from config or Europe/Oslo)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt (specific mode)")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "find / show the booking but don't create it")
	return cmd
}

// resolveTZ chooses tz from --time-zone, then config quick_book.time_zone, else
// Europe/Oslo. Returns the loaded location and the IANA name to send to Robin.
func resolveTZ(name string) (*time.Location, string, error) {
	if name == "" {
		if cfg, err := config.Load(); err == nil && cfg.QuickBook != nil && cfg.QuickBook.TimeZone != "" {
			name = cfg.QuickBook.TimeZone
		}
	}
	if name == "" {
		name = "Europe/Oslo"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, "", fmt.Errorf("invalid --time-zone %q: %w", name, err)
	}
	return loc, name, nil
}

// ---- specific (--space) mode ----

type runSpecificBookArgs struct {
	spaceID                          int64
	startStr, endStr, durationStr    string
	title, description, tzName       string
	tz                               *time.Location
	yes, dryRun                      bool
}

func runSpecificBook(io *IO, a runSpecificBookArgs) error {
	if a.startStr == "" {
		return fmt.Errorf("--start is required with --space")
	}
	if (a.endStr == "") == (a.durationStr == "") {
		return fmt.Errorf("provide exactly one of --end or --duration with --space")
	}
	now := time.Now().In(a.tz)
	start, err := parseWhenOrTime(a.startStr, now, a.tz)
	if err != nil {
		return fmt.Errorf("invalid --start: %w", err)
	}
	var end time.Time
	if a.endStr != "" {
		end, err = parseWhenOrTime(a.endStr, now, a.tz)
		if err != nil {
			return fmt.Errorf("invalid --end: %w", err)
		}
	} else {
		dur, err := time.ParseDuration(a.durationStr)
		if err != nil {
			return fmt.Errorf("invalid --duration: %w", err)
		}
		end = start.Add(dur)
	}
	if !end.After(start) {
		return fmt.Errorf("end must be after start")
	}
	if end.Sub(start) < 5*time.Minute {
		return fmt.Errorf("Robin requires events to be at least 5 minutes")
	}

	sLocal := start.In(a.tz)
	eLocal := end.In(a.tz)
	io.Status("Booking space %d — %s to %s (%v)",
		a.spaceID, sLocal.Format("Mon Jan 2 15:04"),
		eLocal.Format("15:04"), eLocal.Sub(sLocal))

	if a.dryRun {
		if io.JSON {
			return io.JSONOut(map[string]any{
				"dry_run":          true,
				"space_id":         a.spaceID,
				"start":            sLocal.Format(time.RFC3339),
				"end":              eLocal.Format(time.RFC3339),
				"duration_minutes": int(eLocal.Sub(sLocal) / time.Minute),
			})
		}
		io.Success("would book space %d", a.spaceID)
		return nil
	}

	if !a.yes && io.StdinTTY() && !io.NoInput {
		if !confirm(io, "Proceed?") {
			return fmt.Errorf("aborted")
		}
	} else if !a.yes && (io.NoInput || !io.StdinTTY()) {
		return fmt.Errorf("refusing to book without --yes (no interactive terminal)")
	}

	c, err := authedClient(io)
	if err != nil {
		return err
	}
	req := robin.BookRequest{
		Title:       a.title,
		Description: a.description,
		Start:       robin.DateTime{DateTime: sLocal.Format(time.RFC3339), TimeZone: a.tzName},
		End:         robin.DateTime{DateTime: eLocal.Format(time.RFC3339), TimeZone: a.tzName},
	}
	ev, err := c.BookSpace(a.spaceID, req)
	if err != nil {
		return err
	}
	if io.JSON {
		return io.JSONOut(map[string]any{
			"event_id":         ev.ID,
			"space_id":         a.spaceID,
			"start":            sLocal.Format(time.RFC3339),
			"end":              eLocal.Format(time.RFC3339),
			"duration_minutes": int(eLocal.Sub(sLocal) / time.Minute),
		})
	}
	io.Success("booked event %s", ev.ID)
	return nil
}

// ---- auto-pick mode ----

type runAutoPickArgs struct {
	startStr, durationStr, title, description, tzName string
	tz                                                *time.Location
	minMin, maxMin, windowMin                         int
	prioritizeLength, dryRun                          bool
}

func runAutoPickBook(io *IO, a runAutoPickArgs) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	qb := cfg.QuickBook
	if qb == nil || qb.Location == 0 {
		return fmt.Errorf("auto-pick requires quick_book in config. See %s", boldCmd("robin book --help"))
	}
	if !a.prioritizeLength && len(qb.Priority) == 0 {
		return fmt.Errorf("quick_book.priority is empty; set one or pass --prioritize-length")
	}

	// Configured durations / window
	var fixedDur time.Duration
	if a.durationStr != "" {
		fixedDur, err = time.ParseDuration(a.durationStr)
		if err != nil {
			return fmt.Errorf("invalid --duration: %w", err)
		}
		if fixedDur < 5*time.Minute {
			return fmt.Errorf("Robin requires events to be at least 5 minutes")
		}
	}
	minDur := time.Duration(coalesce(a.minMin, qb.MinDurationMinutes, 30)) * time.Minute
	maxDur := time.Duration(coalesce(a.maxMin, qb.MaxDurationMinutes, 120)) * time.Minute
	if fixedDur != 0 {
		minDur, maxDur = fixedDur, fixedDur
	}
	window := time.Duration(coalesce(a.windowMin, qb.WindowMinutes, 30)) * time.Minute
	if minDur < 5*time.Minute {
		return fmt.Errorf("--min must be at least 5 minutes")
	}
	if maxDur < minDur {
		return fmt.Errorf("--max (%v) must be ≥ --min (%v)", maxDur, minDur)
	}

	// Resolve --start (earliest acceptable start)
	from := time.Now().In(a.tz)
	if a.startStr != "" {
		from, err = parseWhenOrTime(a.startStr, time.Now().In(a.tz), a.tz)
		if err != nil {
			return fmt.Errorf("invalid --start: %w", err)
		}
	}
	if from.Second() != 0 || from.Nanosecond() != 0 {
		from = from.Truncate(time.Minute).Add(time.Minute)
	}
	latestStart := from.Add(window)
	queryEnd := latestStart.Add(maxDur)

	c, err := authedClient(io)
	if err != nil {
		return err
	}
	spaces, err := c.Spaces(qb.Location)
	if err != nil {
		return err
	}
	byNum := map[int]robin.Space{}
	for _, s := range spaces {
		if s.IsDisabled {
			continue
		}
		if n, ok := roomNumberOf(s.Name); ok {
			byNum[n] = s
		}
	}
	priIdx := map[int64]int{}
	for i, n := range qb.Priority {
		if s, ok := byNum[n]; ok {
			priIdx[s.ID] = i
		}
	}

	toCheck := priorityRooms(qb.Priority, byNum)
	for _, s := range nonPriorityMeetingRooms(spaces, priIdx) {
		toCheck = append(toCheck, s)
	}

	results := checkSlotsWithProgress(io, c, toCheck, from, latestStart, queryEnd, minDur, maxDur, priIdx)

	priAvail := false
	for _, r := range results {
		if r.available && r.priorityRank < math.MaxInt {
			priAvail = true
			break
		}
	}
	var candidates []slot
	for _, r := range results {
		if !r.available {
			continue
		}
		if !a.prioritizeLength && priAvail && r.priorityRank == math.MaxInt {
			continue
		}
		candidates = append(candidates, r)
	}

	if io.Verbose {
		logSlots(io, results, a.tz)
	}

	if len(candidates) == 0 {
		return fmt.Errorf("no rooms available in the next %v from %s",
			window, from.Format("15:04"))
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		ai, bj := candidates[i], candidates[j]
		if !a.prioritizeLength {
			aPri := ai.priorityRank < math.MaxInt
			bPri := bj.priorityRank < math.MaxInt
			if aPri != bPri {
				return aPri
			}
		}
		if ai.duration != bj.duration {
			return ai.duration > bj.duration
		}
		return ai.priorityRank < bj.priorityRank
	})
	best := candidates[0]
	startLocal := best.start.In(a.tz)
	endLocal := startLocal.Add(best.duration)

	if a.dryRun {
		if io.JSON {
			return io.JSONOut(autoPickJSON(true, "", best, startLocal, endLocal))
		}
		io.Success("would book %s — %s to %s (%v)",
			best.space.Name,
			startLocal.Format("Mon Jan 2 15:04"),
			endLocal.Format("15:04"),
			best.duration)
		return nil
	}

	title := a.title
	if title == "" {
		title = qb.Title
	}
	req := robin.BookRequest{
		Title:       title,
		Description: a.description,
		Start:       robin.DateTime{DateTime: startLocal.Format(time.RFC3339), TimeZone: a.tzName},
		End:         robin.DateTime{DateTime: endLocal.Format(time.RFC3339), TimeZone: a.tzName},
	}
	ev, err := c.BookSpace(best.space.ID, req)
	if err != nil {
		return fmt.Errorf("book %s: %w", best.space.Name, err)
	}
	if io.JSON {
		return io.JSONOut(autoPickJSON(false, ev.ID, best, startLocal, endLocal))
	}
	io.Success("booked %s — %s to %s (%v)",
		best.space.Name,
		startLocal.Format("Mon Jan 2 15:04"),
		endLocal.Format("15:04"),
		best.duration)
	io.Status("event id: %s", ev.ID)
	return nil
}

func autoPickJSON(dryRun bool, eventID string, s slot, start, end time.Time) map[string]any {
	out := map[string]any{
		"dry_run":          dryRun,
		"space_id":         s.space.ID,
		"space_name":       s.space.Name,
		"start":            start.Format(time.RFC3339),
		"end":              end.Format(time.RFC3339),
		"duration_minutes": int(s.duration / time.Minute),
	}
	if eventID != "" {
		out["event_id"] = eventID
	}
	return out
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

