// Package calendar fetches and parses iCalendar (RFC 5545) feeds for use as
// input to `robin book` — each calendar event becomes a row in the picker
// the user can attach a meeting room to.
//
// Scope is intentionally narrow:
//   - VEVENT only (no VTODO, VJOURNAL).
//   - UID, SUMMARY, DTSTART, DTEND, STATUS, RRULE are the recognized props.
//   - Recurring events (RRULE present) are skipped with a warning. v1.
//   - All-day events (DTSTART;VALUE=DATE) are skipped — not real meetings.
//   - CANCELLED events are skipped.
//   - Authentication: URLs are fetched unauthenticated. Tokens in URL work.
package calendar

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/simenandre/robin-cli/internal/config"
)

// Event is a single meeting from a user calendar.
type Event struct {
	UID    string
	Title  string
	Start  time.Time
	End    time.Time
	Source string // calendar name from config
}

// FetchAll pulls every configured calendar and returns the merged event list
// filtered to [now, now+window]. Warnings collect human-readable notes about
// things we skipped (recurring events, parse errors on individual events).
func FetchAll(ctx context.Context, sources []config.CalendarSource, now time.Time, window time.Duration, fallbackTZ *time.Location) ([]Event, []string, error) {
	var all []Event
	var warnings []string
	end := now.Add(window)
	for _, s := range sources {
		evs, warns, err := fetchOne(ctx, s, fallbackTZ)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("calendar %q: %v", s.Name, err))
			continue
		}
		warnings = append(warnings, warns...)
		for _, ev := range evs {
			if ev.End.Before(now) || ev.Start.After(end) {
				continue
			}
			all = append(all, ev)
		}
	}
	return all, warnings, nil
}

func fetchOne(ctx context.Context, src config.CalendarSource, fallbackTZ *time.Location) ([]Event, []string, error) {
	url := src.URL
	if strings.HasPrefix(url, "webcal://") {
		url = "https://" + strings.TrimPrefix(url, "webcal://")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "text/calendar, */*;q=0.5")
	req.Header.Set("User-Agent", "robin-cli (+https://github.com/simenandre/robin-cli)")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return Parse(resp.Body, src.Name, fallbackTZ)
}

// Parse reads an iCalendar stream and returns the parseable VEVENTs plus
// any warnings about skipped/unparseable items.
func Parse(r io.Reader, sourceName string, fallbackTZ *time.Location) ([]Event, []string, error) {
	lines, err := unfold(r)
	if err != nil {
		return nil, nil, err
	}
	var events []Event
	var warnings []string

	type vevent struct {
		UID, Title, Status, RRULE string
		Start, End                time.Time
		AllDay                    bool
		HasStart, HasEnd          bool
	}
	var cur *vevent
	for _, line := range lines {
		switch {
		case line == "BEGIN:VEVENT":
			cur = &vevent{}
		case line == "END:VEVENT":
			if cur == nil {
				continue
			}
			label := cur.Title
			if label == "" {
				label = cur.UID
			}
			switch {
			case cur.RRULE != "":
				warnings = append(warnings, fmt.Sprintf("[%s] skipped recurring event: %s", sourceName, label))
			case strings.EqualFold(cur.Status, "CANCELLED"):
				// silently drop
			case cur.AllDay:
				// silently drop — all-day events aren't meeting bookings
			case !cur.HasStart || !cur.HasEnd:
				warnings = append(warnings, fmt.Sprintf("[%s] skipped event without start/end: %s", sourceName, label))
			default:
				events = append(events, Event{
					UID:    cur.UID,
					Title:  cur.Title,
					Start:  cur.Start,
					End:    cur.End,
					Source: sourceName,
				})
			}
			cur = nil
		case cur != nil:
			key, params, val := splitProperty(line)
			switch strings.ToUpper(key) {
			case "UID":
				cur.UID = val
			case "SUMMARY":
				cur.Title = unescapeText(val)
			case "STATUS":
				cur.Status = strings.ToUpper(val)
			case "RRULE":
				cur.RRULE = val
			case "DTSTART":
				t, allDay, err := parseDateTime(params, val, fallbackTZ)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("[%s] DTSTART parse: %v", sourceName, err))
					continue
				}
				cur.Start = t
				cur.AllDay = allDay
				cur.HasStart = true
			case "DTEND":
				t, _, err := parseDateTime(params, val, fallbackTZ)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("[%s] DTEND parse: %v", sourceName, err))
					continue
				}
				cur.End = t
				cur.HasEnd = true
			}
		}
	}
	return events, warnings, nil
}

// unfold reads lines, joining continuation lines (start with space or tab)
// back onto the previous logical line per RFC 5545 §3.1.
func unfold(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []string
	for sc.Scan() {
		line := sc.Text()
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if len(out) > 0 && (line[0] == ' ' || line[0] == '\t') {
			out[len(out)-1] += line[1:]
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
}

// splitProperty splits "DTSTART;TZID=Europe/Oslo:20260525T140000" into
// key="DTSTART", params=map{"TZID":"Europe/Oslo"}, value="20260525T140000".
func splitProperty(line string) (key string, params map[string]string, value string) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return line, nil, ""
	}
	left := line[:colon]
	value = line[colon+1:]
	params = map[string]string{}
	if semi := strings.IndexByte(left, ';'); semi >= 0 {
		key = left[:semi]
		for _, kv := range strings.Split(left[semi+1:], ";") {
			if eq := strings.IndexByte(kv, '='); eq > 0 {
				params[strings.ToUpper(kv[:eq])] = strings.Trim(kv[eq+1:], `"`)
			}
		}
	} else {
		key = left
	}
	return key, params, value
}

// parseDateTime handles the three DTSTART/DTEND value forms:
//   - VALUE=DATE → "20260525" (all-day; allDay=true)
//   - UTC → "20260525T140000Z"
//   - local with TZID → "20260525T140000" + ;TZID=Europe/Oslo
//   - floating (no TZID, no Z) → interpreted in fallbackTZ
func parseDateTime(params map[string]string, value string, fallbackTZ *time.Location) (time.Time, bool, error) {
	if params["VALUE"] == "DATE" || (len(value) == 8 && !strings.ContainsAny(value, "T")) {
		t, err := time.ParseInLocation("20060102", value, fallbackTZ)
		return t, true, err
	}
	if strings.HasSuffix(value, "Z") {
		t, err := time.ParseInLocation("20060102T150405Z", value, time.UTC)
		return t, false, err
	}
	loc := fallbackTZ
	if tz := params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	t, err := time.ParseInLocation("20060102T150405", value, loc)
	return t, false, err
}

// unescapeText reverses the iCal text escapes used in SUMMARY/DESCRIPTION.
func unescapeText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n', 'N':
				b.WriteByte('\n')
			case ',', ';', '\\':
				b.WriteByte(s[i+1])
			default:
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
