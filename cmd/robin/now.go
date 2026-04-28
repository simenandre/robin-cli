package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/simenandre/robin-user-api/internal/config"
	"github.com/simenandre/robin-user-api/internal/robin"
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
	priorityRank int // index in priority list, math.MaxInt if not listed
	start        time.Time
	duration     time.Duration
	available    bool
	err          error
}

func cmdNow() error {
	fs := flag.NewFlagSet("now", flag.ExitOnError)
	minFlag := fs.Int("min", 0, "minimum duration in minutes")
	maxFlag := fs.Int("max", 0, "maximum duration in minutes")
	winFlag := fs.Int("window", 0, "search window minutes")
	whenFlag := fs.String("when", "", "start search from: '1h', '30m', '14:00', or '2026-04-29 09:00' (default: now)")
	dryRun := fs.Bool("dry-run", false, "find a room but don't book")
	titleFlag := fs.String("title", "", "event title")
	prioritizeLength := fs.Bool("prioritize-length", false, "rank by available length, ignore priority order")
	fs.Bool("verbose", false, "")
	fs.Bool("v", false, "")
	_ = fs.Parse(os.Args[2:])

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	qb := cfg.QuickBook
	if qb == nil || qb.Location == 0 {
		return fmt.Errorf("config missing quick_book section — see %s", configHint())
	}
	if !*prioritizeLength && len(qb.Priority) == 0 {
		return fmt.Errorf("config has no quick_book.priority — set one or use --prioritize-length")
	}

	minDur := time.Duration(coalesce(*minFlag, qb.MinDurationMinutes, 30)) * time.Minute
	maxDur := time.Duration(coalesce(*maxFlag, qb.MaxDurationMinutes, 120)) * time.Minute
	window := time.Duration(coalesce(*winFlag, qb.WindowMinutes, 30)) * time.Minute
	if minDur < 5*time.Minute {
		return fmt.Errorf("min duration must be ≥ 5 minutes")
	}
	if maxDur < minDur {
		return fmt.Errorf("max (%v) must be ≥ min (%v)", maxDur, minDur)
	}

	tzName := qb.TimeZone
	if tzName == "" {
		tzName = "Europe/Oslo"
	}
	tz, err := time.LoadLocation(tzName)
	if err != nil {
		return fmt.Errorf("bad time_zone %q: %w", tzName, err)
	}

	c, err := loadAuthed()
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
	priorityIdx := map[int64]int{}
	for i, n := range qb.Priority {
		if s, ok := byNum[n]; ok {
			priorityIdx[s.ID] = i
		}
	}

	from := time.Now().In(tz)
	if *whenFlag != "" {
		from, err = parseWhen(*whenFlag, time.Now().In(tz), tz)
		if err != nil {
			return fmt.Errorf("invalid --when: %w", err)
		}
	}
	if from.Second() != 0 || from.Nanosecond() != 0 {
		from = from.Truncate(time.Minute).Add(time.Minute)
	}
	latestStart := from.Add(window)
	queryEnd := latestStart.Add(maxDur)

	// Phase 1: check priority rooms.
	priorityResults := checkSlots(c, priorityRooms(qb.Priority, byNum), from, latestStart, queryEnd, minDur, maxDur, priorityIdx)
	logSlots(priorityResults, tz)

	priAvailable := filterAvailable(priorityResults)

	// Phase 2: fallback / length-prioritized — check non-priority meeting rooms.
	var fallbackResults []slot
	needFallback := *prioritizeLength || len(priAvailable) == 0
	if needFallback {
		fallbackResults = checkSlots(c, nonPriorityMeetingRooms(spaces, priorityIdx), from, latestStart, queryEnd, minDur, maxDur, priorityIdx)
		logSlots(fallbackResults, tz)
	}

	candidates := append([]slot{}, priAvailable...)
	candidates = append(candidates, filterAvailable(fallbackResults)...)
	if len(candidates) == 0 {
		return fmt.Errorf("no rooms available in the next %v", window)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if !*prioritizeLength {
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
	fmt.Printf("→ %s: %s–%s (%v)\n", best.space.Name,
		startLocal.Format("15:04"), endLocal.Format("15:04"), best.duration)

	if *dryRun {
		return nil
	}

	title := *titleFlag
	if title == "" {
		title = qb.Title
	}
	req := robin.BookRequest{
		Title: title,
		Start: robin.DateTime{
			DateTime: startLocal.Format(time.RFC3339),
			TimeZone: tzName,
		},
		End: robin.DateTime{
			DateTime: endLocal.Format(time.RFC3339),
			TimeZone: tzName,
		},
	}
	ev, err := c.BookSpace(best.space.ID, req)
	if err != nil {
		return fmt.Errorf("book %s: %w", best.space.Name, err)
	}
	fmt.Printf("booked event %s\n", ev.ID)
	return nil
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

func checkSlots(c *robin.Client, spaces []robin.Space, from, latestStart, queryEnd time.Time, minDur, maxDur time.Duration, priorityIdx map[int64]int) []slot {
	results := make([]slot, len(spaces))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, s := range spaces {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, s robin.Space) {
			defer wg.Done()
			defer func() { <-sem }()
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

func filterAvailable(results []slot) []slot {
	out := make([]slot, 0, len(results))
	for _, r := range results {
		if r.available {
			out = append(out, r)
		}
	}
	return out
}

func logSlots(results []slot, tz *time.Location) {
	for _, r := range results {
		switch {
		case r.err != nil:
			fmt.Fprintf(os.Stderr, "  %s: error %v\n", r.space.Name, r.err)
		case r.available:
			fmt.Fprintf(os.Stderr, "  %s: free %s for %v\n", r.space.Name,
				r.start.In(tz).Format("15:04"), r.duration)
		default:
			fmt.Fprintf(os.Stderr, "  %s: busy\n", r.space.Name)
		}
	}
}

// findLongestSlot returns the earliest start time in [from, latestStart] with
// at least minDur of free time, and the maximum duration available from there
// (capped at maxDur).
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

// parseWhen accepts a duration ("1h", "30m"), a clock time today ("14:00"),
// or a full datetime ("2026-04-29 09:00", RFC3339).
func parseWhen(s string, now time.Time, tz *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "now" {
		return now, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(d), nil
	}
	if t, err := time.ParseInLocation("15:04", s, tz); err == nil {
		y, m, d := now.Date()
		return time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, tz), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, tz); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse %q (expected duration like '1h', clock time '14:00', or datetime '2026-04-29 09:00')", s)
}

func coalesce(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func configHint() string {
	dir, _ := config.Dir()
	return dir + "/config.json"
}
