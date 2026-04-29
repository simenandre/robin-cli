package cli

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fatih/color"
	"github.com/simenandre/robin-user-api/internal/config"
	"github.com/simenandre/robin-user-api/internal/robin"
	"github.com/spf13/cobra"
)

var roomNumPattern = regexp.MustCompile(`^Meeting Room (\d+)(\s|$)`)

func roomNumberOf(name string) (int, bool) {
	m := roomNumPattern.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

type slot struct {
	space        robin.Space
	priorityRank int
	start        time.Time
	duration     time.Duration
	available    bool
	err          error
}

func newNowCmd(io *IO) *cobra.Command {
	var (
		whenStr          string
		minMin           int
		maxMin           int
		windowMin        int
		dryRun           bool
		title            string
		prioritizeLength bool
	)

	cmd := &cobra.Command{
		Use:   "now",
		Short: "Book the best available priority room.",
		Long: `Looks at quick_book.priority in your config, finds the room with the
longest free slot (capped at max_duration_minutes), and books it. If
none of your priority rooms are available, falls back to other meeting
rooms in the configured location.

Configure quick_book in your config file before using this:

  ` + configFilePath() + `

  {
    "quick_book": {
      "location": 22847,
      "priority": [6, 7, 5, 4, 11, 10, 8, 9],
      "min_duration_minutes": 30,
      "max_duration_minutes": 120,
      "window_minutes": 30,
      "time_zone": "Europe/Oslo"
    }
  }`,
		Example: `  # book now (or up to 30 min from now)
  robin now

  # see the pick without booking
  robin now --dry-run

  # natural language: tomorrow morning, in 2 hours, next monday at 14:00
  robin now --when tomorrow
  robin now --when "tomorrow 9am"
  robin now --when "in 2 hours"
  robin now --when "next monday at 14:00"

  # strict forms also work
  robin now --when 1h
  robin now --when 14:00
  robin now --when "2026-04-30 09:00"

  # ignore priority, take the longest available room
  robin now --prioritize-length

  # cap booking length at 60 min
  robin now --max 60`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			qb := cfg.QuickBook
			if qb == nil || qb.Location == 0 {
				return fmt.Errorf("config missing quick_book section. See %s", boldCmd("robin now --help"))
			}
			if !prioritizeLength && len(qb.Priority) == 0 {
				return fmt.Errorf("quick_book.priority is empty; set one or pass --prioritize-length")
			}

			minDur := time.Duration(coalesce(minMin, qb.MinDurationMinutes, 30)) * time.Minute
			maxDur := time.Duration(coalesce(maxMin, qb.MaxDurationMinutes, 120)) * time.Minute
			window := time.Duration(coalesce(windowMin, qb.WindowMinutes, 30)) * time.Minute
			if minDur < 5*time.Minute {
				return fmt.Errorf("--min must be at least 5 minutes")
			}
			if maxDur < minDur {
				return fmt.Errorf("--max (%v) must be ≥ --min (%v)", maxDur, minDur)
			}

			tzName := qb.TimeZone
			if tzName == "" {
				tzName = "Europe/Oslo"
			}
			tz, err := time.LoadLocation(tzName)
			if err != nil {
				return fmt.Errorf("bad time_zone %q: %w", tzName, err)
			}

			from := time.Now().In(tz)
			if whenStr != "" {
				from, err = parseWhenOrTime(whenStr, time.Now().In(tz), tz)
				if err != nil {
					return fmt.Errorf("invalid --when: %w", err)
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
			needFallback := prioritizeLength || true // we'll trim later if priority covered it
			if needFallback {
				for _, s := range nonPriorityMeetingRooms(spaces, priIdx) {
					toCheck = append(toCheck, s)
				}
			}

			results := checkSlotsWithProgress(io, c, toCheck, from, latestStart, queryEnd, minDur, maxDur, priIdx)

			// Default mode: only consider non-priority rooms if no priority room had a slot.
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
				if !prioritizeLength && priAvail && r.priorityRank == math.MaxInt {
					continue
				}
				candidates = append(candidates, r)
			}

			if io.Verbose {
				logSlots(io, results, tz)
			}

			if len(candidates) == 0 {
				return fmt.Errorf("no rooms available in the next %v from %s",
					window, from.Format("15:04"))
			}

			sort.SliceStable(candidates, func(i, j int) bool {
				a, b := candidates[i], candidates[j]
				if !prioritizeLength {
					aPri := a.priorityRank < math.MaxInt
					bPri := b.priorityRank < math.MaxInt
					if aPri != bPri {
						return aPri
					}
				}
				if a.duration != b.duration {
					return a.duration > b.duration
				}
				return a.priorityRank < b.priorityRank
			})
			best := candidates[0]
			startLocal := best.start.In(tz)
			endLocal := startLocal.Add(best.duration)

			if dryRun {
				if io.JSON {
					return io.JSONOut(dryRunJSON(best, startLocal, endLocal))
				}
				io.Success("would book %s — %s to %s (%v)",
					best.space.Name,
					startLocal.Format("Mon Jan 2 15:04"),
					endLocal.Format("15:04"),
					best.duration)
				return nil
			}

			if title == "" {
				title = qb.Title
			}
			req := robin.BookRequest{
				Title: title,
				Start: robin.DateTime{DateTime: startLocal.Format(time.RFC3339), TimeZone: tzName},
				End:   robin.DateTime{DateTime: endLocal.Format(time.RFC3339), TimeZone: tzName},
			}
			ev, err := c.BookSpace(best.space.ID, req)
			if err != nil {
				return fmt.Errorf("book %s: %w", best.space.Name, err)
			}
			if io.JSON {
				return io.JSONOut(map[string]any{
					"event_id":         ev.ID,
					"space_id":         best.space.ID,
					"space_name":       best.space.Name,
					"start":            startLocal.Format(time.RFC3339),
					"end":              endLocal.Format(time.RFC3339),
					"duration_minutes": int(best.duration / time.Minute),
				})
			}
			io.Success("booked %s — %s to %s (%v)",
				best.space.Name,
				startLocal.Format("Mon Jan 2 15:04"),
				endLocal.Format("15:04"),
				best.duration)
			io.Status("event id: %s", ev.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&whenStr, "when", "", "search anchor: '1h', '14:00', 'tomorrow 9am', 'in 2 hours', '2026-04-29 09:00' (default: now)")
	cmd.Flags().IntVar(&minMin, "min", 0, "minimum slot length in minutes (default: from config or 30)")
	cmd.Flags().IntVar(&maxMin, "max", 0, "maximum booking length in minutes (default: from config or 120)")
	cmd.Flags().IntVar(&windowMin, "window", 0, "search window in minutes (default: from config or 30)")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "find a room but don't book")
	cmd.Flags().StringVar(&title, "title", "", "event title")
	cmd.Flags().BoolVarP(&prioritizeLength, "prioritize-length", "L", false, "rank by length, ignore priority order")
	return cmd
}

func dryRunJSON(s slot, start, end time.Time) map[string]any {
	return map[string]any{
		"dry_run":          true,
		"space_id":         s.space.ID,
		"space_name":       s.space.Name,
		"start":            start.Format(time.RFC3339),
		"end":              end.Format(time.RFC3339),
		"duration_minutes": int(s.duration / time.Minute),
	}
}

func priorityRooms(priority []int, byNum map[int]robin.Space) []robin.Space {
	out := make([]robin.Space, 0, len(priority))
	seen := map[int64]bool{}
	for _, n := range priority {
		s, ok := byNum[n]
		if !ok || seen[s.ID] {
			continue
		}
		out = append(out, s)
		seen[s.ID] = true
	}
	return out
}

func nonPriorityMeetingRooms(spaces []robin.Space, priorityIdx map[int64]int) []robin.Space {
	out := make([]robin.Space, 0)
	for _, s := range spaces {
		if s.IsDisabled || s.Type != "meeting" {
			continue
		}
		if _, ok := priorityIdx[s.ID]; ok {
			continue
		}
		out = append(out, s)
	}
	return out
}

func checkSlotsWithProgress(io *IO, c *robin.Client, spaces []robin.Space, from, latestStart, queryEnd time.Time, minDur, maxDur time.Duration, priorityIdx map[int64]int) []slot {
	results := make([]slot, len(spaces))
	if len(spaces) == 0 {
		return results
	}
	var done atomic.Int32
	stopProgress := startProgress(io, len(spaces), &done)
	defer stopProgress()

	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, s := range spaces {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, s robin.Space) {
			defer wg.Done()
			defer func() { <-sem; done.Add(1) }()
			rank, ok := priorityIdx[s.ID]
			if !ok {
				rank = math.MaxInt
			}
			events, err := c.SpaceEvents(s.ID, from, queryEnd)
			if err != nil {
				results[i] = slot{space: s, priorityRank: rank, err: err}
				return
			}
			start, dur, ok := findLongestSlot(events, from, latestStart, minDur, maxDur)
			results[i] = slot{space: s, priorityRank: rank, start: start, duration: dur, available: ok}
		}(i, s)
	}
	wg.Wait()
	return results
}

// startProgress prints "checking N rooms..." then a spinner that updates every
// 100ms with current/total. Returns a stop func that clears the line.
func startProgress(io *IO, total int, done *atomic.Int32) func() {
	if io.Quiet || io.JSON || !io.stderrTTY {
		return func() {}
	}
	stop := make(chan struct{})
	go func() {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		dim := color.New(color.Faint).SprintFunc()
		for {
			select {
			case <-stop:
				fmt.Fprint(io.Err, "\r\033[K")
				return
			default:
				fmt.Fprintf(io.Err, "\r%s checking rooms (%d/%d)", dim(frames[i%len(frames)]), done.Load(), total)
				i++
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	return func() { close(stop); time.Sleep(120 * time.Millisecond) }
}

func logSlots(io *IO, results []slot, tz *time.Location) {
	for _, r := range results {
		switch {
		case r.err != nil:
			io.Debug("%s: error %v", r.space.Name, r.err)
		case r.available:
			io.Debug("%s: free %s for %v", r.space.Name, r.start.In(tz).Format("15:04"), r.duration)
		default:
			io.Debug("%s: busy", r.space.Name)
		}
	}
}

func findLongestSlot(events []robin.Event, from, latestStart time.Time, minDur, maxDur time.Duration) (time.Time, time.Duration, bool) {
	type iv struct{ start, end time.Time }
	intervals := make([]iv, 0, len(events))
	for _, ev := range events {
		s, err1 := ev.Start.Time()
		e, err2 := ev.End.Time()
		if err1 != nil || err2 != nil {
			continue
		}
		intervals = append(intervals, iv{s, e})
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start.Before(intervals[j].start) })

	candidate := from
	for _, x := range intervals {
		if !x.end.After(candidate) {
			continue
		}
		if x.start.After(candidate) {
			gap := x.start.Sub(candidate)
			if gap >= minDur && !candidate.After(latestStart) {
				dur := gap
				if dur > maxDur {
					dur = maxDur
				}
				return candidate, dur, true
			}
		}
		candidate = x.end
	}
	if !candidate.After(latestStart) {
		return candidate, maxDur, true
	}
	return time.Time{}, 0, false
}

func coalesce(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}
