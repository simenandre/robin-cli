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
	"github.com/simenandre/robin-user-api/internal/robin"
)

// Picker logic for auto-pick bookings: find the best meeting room for a slot
// at or after `from`, within a window, given a priority list.

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

func coalesce(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}
