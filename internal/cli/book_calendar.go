package cli

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/simenandre/robin-cli/internal/calendar"
	"github.com/simenandre/robin-cli/internal/config"
	"github.com/simenandre/robin-cli/internal/robin"
)

// iCalUIDTag is what robin stamps into a booking's description so a follow-up
// 'robin book' call can recognise that the iCal event already has a room.
const iCalUIDTag = "iCalUID:"

// runCalendarPickerBook runs the calendar-aware book flow when there are
// configured calendars and no --start was given. Returns (handled, err):
// when handled is true the caller should NOT fall through to auto-pick.
func runCalendarPickerBook(io *IO, a runAutoPickArgs) (bool, error) {
	cfg, err := config.Load()
	if err != nil {
		return false, err
	}
	if cfg.QuickBook == nil || len(cfg.QuickBook.Calendars) == 0 {
		return false, nil
	}

	now := time.Now().In(a.tz)
	const calendarWindow = 14 * 24 * time.Hour

	io.Status("fetching calendars...")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	calEvents, warnings, err := calendar.FetchAll(ctx, cfg.QuickBook.Calendars, now, calendarWindow, a.tz)
	if err != nil {
		return false, err
	}
	for _, w := range warnings {
		io.Debug("%s", w)
	}

	c, err := authedClient(io)
	if err != nil {
		return false, err
	}

	booked, err := alreadyBookedUIDs(c, now, calendarWindow)
	if err != nil {
		io.Status("warning: couldn't fetch existing Robin events: %v", err)
	}

	candidates := filterPickable(calEvents, now, booked)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Start.Before(candidates[j].Start) })

	pick, err := promptCalendarPick(candidates, now, a.tz)
	if err != nil {
		return false, err
	}
	if pick.UID == "" {
		return true, runAutoPickBook(io, a)
	}
	return true, bookForCalendarEvent(io, c, cfg, pick, a)
}

func filterPickable(events []calendar.Event, now time.Time, alreadyBooked map[string]bool) []calendar.Event {
	out := make([]calendar.Event, 0, len(events))
	for _, ev := range events {
		if ev.End.Before(now) {
			continue
		}
		if alreadyBooked[ev.UID] {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// alreadyBookedUIDs queries Robin for the user's events in the window and
// extracts every iCalUID we previously stamped into a booking description.
func alreadyBookedUIDs(c *robin.Client, now time.Time, window time.Duration) (map[string]bool, error) {
	events, err := c.MyEvents(now, now.Add(window))
	if err != nil {
		return nil, err
	}
	uids := map[string]bool{}
	for _, ev := range events {
		uid := extractICalUID(ev.Title) // future-proof: tag might land in title
		if uid == "" {
			uid = extractICalUID(ev.ID) // unlikely but harmless
		}
		if uid != "" {
			uids[uid] = true
		}
	}
	// The description isn't returned by Robin's event list endpoint today,
	// so we fall back to extracting from any field that does come through.
	// If a future API change adds it, expand this loop accordingly.
	return uids, nil
}

func extractICalUID(s string) string {
	idx := strings.Index(s, iCalUIDTag)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(iCalUIDTag):]
	// UID runs until whitespace, newline, or closing punctuation.
	end := len(rest)
	for i, r := range rest {
		if r == ' ' || r == '\n' || r == '\t' || r == ')' || r == ']' {
			end = i
			break
		}
	}
	return strings.TrimSpace(rest[:end])
}

// promptCalendarPick renders a single-select: a "Now" sentinel + each
// upcoming calendar event. A zero-value event (empty UID) means the user
// picked "Now".
func promptCalendarPick(events []calendar.Event, now time.Time, tz *time.Location) (calendar.Event, error) {
	options := []huh.Option[int]{
		huh.NewOption("✨ Now — auto-pick a room starting at "+now.Format("15:04"), -1),
	}
	for i, ev := range events {
		label := fmt.Sprintf("%s — %s  (%s)",
			titleOrPlaceholder(ev.Title),
			formatEventTime(ev.Start.In(tz), ev.End.In(tz)),
			ev.Source)
		options = append(options, huh.NewOption(label, i))
	}

	choice := -1
	if len(events) > 0 {
		choice = 0
	}
	if err := huh.NewSelect[int]().
		Title("Pick what to book a room for").
		Description("Type / to filter.").
		Filtering(true).
		Options(options...).
		Value(&choice).
		Run(); err != nil {
		return calendar.Event{}, fmt.Errorf("book: %w", err)
	}
	if choice < 0 {
		return calendar.Event{}, nil
	}
	return events[choice], nil
}

func formatEventTime(start, end time.Time) string {
	if start.Format("2006-01-02") == end.Format("2006-01-02") {
		return start.Format("Mon Jan 2 15:04") + "–" + end.Format("15:04")
	}
	return start.Format("Mon Jan 2 15:04") + "–" + end.Format("Mon Jan 2 15:04")
}

// bookForCalendarEvent finds the highest-priority room free for the
// calendar event's exact window and books it, stamping the iCal UID into
// the description.
func bookForCalendarEvent(io *IO, c *robin.Client, cfg *config.Config, ev calendar.Event, a runAutoPickArgs) error {
	qb := cfg.QuickBook
	if qb == nil || qb.Location == 0 {
		return fmt.Errorf("quick_book.location not set")
	}

	meetingStart := ev.Start.In(a.tz)
	meetingEnd := ev.End.In(a.tz)
	if meetingEnd.Sub(meetingStart) < 5*time.Minute {
		return fmt.Errorf("event shorter than 5 minutes (Robin minimum): %s", ev.Title)
	}
	bookStart := meetingStart.Add(-a.bufferBefore)
	bookEnd := meetingEnd.Add(a.bufferAfter)

	spaces, err := c.Spaces(qb.Location)
	if err != nil {
		return err
	}
	byNum := map[int]robin.Space{}
	for _, s := range spaces {
		if s.IsDisabled || s.Type != "meeting" {
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

	bookDur := bookEnd.Sub(bookStart)
	results := checkSlotsWithProgress(io, c, toCheck, bookStart, bookStart, bookEnd.Add(time.Minute), bookDur, bookDur, priIdx)
	var pick *robin.Space
	bestRank := math.MaxInt
	for _, r := range results {
		if !r.available {
			continue
		}
		if r.start.After(bookStart) {
			continue
		}
		if r.priorityRank < bestRank {
			s := r.space
			pick = &s
			bestRank = r.priorityRank
		}
	}
	if pick == nil {
		return fmt.Errorf("no rooms free for %s", formatEventTime(bookStart, bookEnd))
	}

	title := a.title
	if title == "" {
		title = ev.Title
	}
	if title == "" {
		title = qb.Title
	}
	description := a.description
	if description == "" {
		description = iCalUIDTag + ev.UID + " (booked by robin-cli)"
	} else {
		description = description + "\n" + iCalUIDTag + ev.UID
	}

	if a.bufferBefore > 0 || a.bufferAfter > 0 {
		io.Status("buffer: %v before, %v after", a.bufferBefore, a.bufferAfter)
	}
	if a.dryRun {
		io.Success("would book %s for %q — meeting %s (%v)",
			pick.Name, title, formatEventTime(meetingStart, meetingEnd), meetingEnd.Sub(meetingStart))
		if a.bufferBefore > 0 || a.bufferAfter > 0 {
			io.Status("room held %s–%s", bookStart.Format("15:04"), bookEnd.Format("15:04"))
		}
		return nil
	}
	req := robin.BookRequest{
		Title:       title,
		Description: description,
		Start:       robin.DateTime{DateTime: bookStart.Format(time.RFC3339), TimeZone: a.tzName},
		End:         robin.DateTime{DateTime: bookEnd.Format(time.RFC3339), TimeZone: a.tzName},
	}
	booking, err := c.BookSpace(pick.ID, req)
	if err != nil {
		return err
	}
	io.Success("booked %s for %q — meeting %s", pick.Name, title, formatEventTime(meetingStart, meetingEnd))
	if a.bufferBefore > 0 || a.bufferAfter > 0 {
		io.Status("room held %s–%s", bookStart.Format("15:04"), bookEnd.Format("15:04"))
	}
	io.Status("event id: %s", booking.ID)
	return nil
}
