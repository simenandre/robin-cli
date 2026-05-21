package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Org       string           `json:"org"`
	Email     string           `json:"email"`
	Password  string           `json:"password"`
	QuickBook *QuickBookConfig `json:"quick_book,omitempty"`
}

type QuickBookConfig struct {
	Location            int64            `json:"location"`
	Priority            []int            `json:"priority"`
	MinDurationMinutes  int              `json:"min_duration_minutes"`
	MaxDurationMinutes  int              `json:"max_duration_minutes"`
	WindowMinutes       int              `json:"window_minutes"`
	BufferBeforeMinutes int              `json:"buffer_before_minutes,omitempty"`
	BufferAfterMinutes  int              `json:"buffer_after_minutes,omitempty"`
	TimeZone            string           `json:"time_zone"`
	Title               string           `json:"title,omitempty"`
	WorkingHours        *WorkingHours    `json:"working_hours,omitempty"`
	Calendars           []CalendarSource `json:"calendars,omitempty"`
}

// CalendarSource is one iCal feed robin reads to pre-populate the book
// picker with the user's upcoming meetings.
type CalendarSource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// WorkingHours bound the workday — auto-pick uses these to snap a --start
// that lands before Start up to Start on the same day, so 'robin book
// --start tomorrow' (which resolves to tomorrow 00:00) finds a slot inside
// office hours.
type WorkingHours struct {
	Start string `json:"start"` // "HH:MM"
	End   string `json:"end"`   // "HH:MM"
}

// Snap returns t bumped up to the start of working hours when it falls on
// the same day before Start. t at-or-after Start is returned unchanged —
// auto-pick handles the past-End case naturally by not finding any rooms.
func (w *WorkingHours) Snap(t time.Time) (time.Time, error) {
	if w == nil || w.Start == "" || w.End == "" {
		return t, nil
	}
	sh, sm, err := ParseClockTime(w.Start)
	if err != nil {
		return t, fmt.Errorf("working_hours.start: %w", err)
	}
	eh, em, err := ParseClockTime(w.End)
	if err != nil {
		return t, fmt.Errorf("working_hours.end: %w", err)
	}
	y, m, d := t.Date()
	loc := t.Location()
	start := time.Date(y, m, d, sh, sm, 0, 0, loc)
	end := time.Date(y, m, d, eh, em, 0, 0, loc)
	if !end.After(start) {
		return t, fmt.Errorf("working_hours: end (%s) must be after start (%s)", w.End, w.Start)
	}
	if t.Before(start) {
		return start, nil
	}
	return t, nil
}

// ParseClockTime parses "HH:MM" → (h, m). Exposed so the interactive picker
// can validate input as the user types.
func ParseClockTime(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("invalid HH:MM %q", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("HH:MM out of range %q", s)
	}
	return h, m, nil
}

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "robin"), nil
}

func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func Load() (*Config, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no config at %s — run `robin init` first", p)
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if c.Org == "" || c.Email == "" || c.Password == "" {
		return nil, fmt.Errorf("config %s is missing org/email/password", p)
	}
	return &c, nil
}

func Save(c *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	p, err := path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}
