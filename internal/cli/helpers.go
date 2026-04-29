package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/simenandre/robin-user-api/internal/robin"
	naturaldate "github.com/tj/go-naturaldate"
)

// authedClient returns a Robin client backed by the cached session.
func authedClient(io *IO) (*robin.Client, error) {
	sess, err := robin.LoadSession()
	if err != nil {
		return nil, err
	}
	if sess == nil || sess.AccessToken == "" {
		return nil, fmt.Errorf("not authenticated; run %s", boldCmd("robin login"))
	}
	c := robin.New().WithSession(sess)
	c.Verbose = io.Verbose
	return c, nil
}

// parseWhenOrTime resolves a user-supplied time expression. Tried in order:
//
//  1. "now" or empty → base
//  2. Go duration: "1h", "30m", "2h30m" → base + d
//  3. Clock time today: "14:00", "09:30"
//  4. Strict datetime: RFC3339, "2006-01-02 15:04", "2006-01-02T15:04",
//     "2006-01-02 15:04:05"
//  5. Natural language (English): "tomorrow", "tomorrow 9am",
//     "next monday at 14:00", "in 2 hours", "noon", "9am" — handled by
//     github.com/tj/go-naturaldate, which mirrors GNU date -d / `at` style.
//
// All resolved times are anchored to the supplied tz when the input itself
// doesn't carry an offset.
func parseWhenOrTime(s string, base time.Time, tz *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "now") {
		return base, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return base.Add(d), nil
	}
	if t, err := time.ParseInLocation("15:04", s, tz); err == nil {
		y, m, day := base.Date()
		return time.Date(y, m, day, t.Hour(), t.Minute(), 0, 0, tz), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, tz); err == nil {
			return t, nil
		}
	}
	// naturaldate defaults ambiguous phrases ("monday", "april") to the past.
	// For a meeting-booking CLI, "monday" almost always means the upcoming
	// Monday — so resolve forward.
	if t, err := naturaldate.Parse(s, base, naturaldate.WithDirection(naturaldate.Future)); err == nil && !t.IsZero() && !t.Equal(base) {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("could not parse %q (try '1h', '14:00', 'tomorrow 9am', or '2026-04-29 09:00')", s)
}

func boldCmd(s string) string {
	if color.NoColor {
		return "`" + s + "`"
	}
	return color.New(color.Bold).Sprint(s)
}

func faintInt(n int64) string {
	if color.NoColor {
		return fmt.Sprintf("space %d", n)
	}
	return color.New(color.Faint).Sprintf("space %d", n)
}
