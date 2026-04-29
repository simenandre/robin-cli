package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/simenandre/robin-user-api/internal/robin"
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

// parseWhenOrTime accepts:
//   - "now"
//   - a duration: "1h", "30m", "2h30m" (relative to base)
//   - clock time today: "14:00"
//   - full datetime: "2026-04-29 09:00", "2026-04-29T09:00", RFC3339
func parseWhenOrTime(s string, base time.Time, tz *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "now" {
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
	return time.Time{}, fmt.Errorf("could not parse %q (expected '1h', '14:00', or '2026-04-29 09:00')", s)
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
