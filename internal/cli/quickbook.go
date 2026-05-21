package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/simenandre/robin-cli/internal/config"
	"github.com/simenandre/robin-cli/internal/robin"
)

// commonTimeZones is the dropdown shown in the time-zone picker. Anything
// outside this list can be entered via the "Custom..." option.
var commonTimeZones = []string{
	"Europe/Oslo",
	"Europe/Stockholm",
	"Europe/Copenhagen",
	"Europe/Helsinki",
	"Europe/London",
	"Europe/Berlin",
	"Europe/Paris",
	"Europe/Madrid",
	"America/New_York",
	"America/Chicago",
	"America/Denver",
	"America/Los_Angeles",
	"UTC",
}

// runQuickBookSetup walks the user through every quick_book field. current is
// the existing config (may be nil). The returned config is what should be
// saved.
func runQuickBookSetup(c *robin.Client, current *config.QuickBookConfig) (*config.QuickBookConfig, error) {
	out := &config.QuickBookConfig{}
	if current != nil {
		*out = *current
	}

	orgID, err := pickOrg(c)
	if err != nil {
		return nil, err
	}
	locID, err := pickLocation(c, orgID, out.Location)
	if err != nil {
		return nil, err
	}
	out.Location = locID

	priority, err := pickPriorityRooms(c, locID, out.Priority)
	if err != nil {
		return nil, err
	}
	out.Priority = priority

	tz, err := pickTimeZone(out.TimeZone)
	if err != nil {
		return nil, err
	}
	out.TimeZone = tz

	min, max, window, err := pickDurations(out.MinDurationMinutes, out.MaxDurationMinutes, out.WindowMinutes)
	if err != nil {
		return nil, err
	}
	out.MinDurationMinutes = min
	out.MaxDurationMinutes = max
	out.WindowMinutes = window

	wh, err := pickWorkingHours(out.WorkingHours)
	if err != nil {
		return nil, err
	}
	out.WorkingHours = wh

	title, err := pickTitle(out.Title)
	if err != nil {
		return nil, err
	}
	out.Title = title
	return out, nil
}

func pickOrg(c *robin.Client) (int64, error) {
	orgs, err := c.MyOrganizations()
	if err != nil {
		return 0, err
	}
	if len(orgs) == 0 {
		return 0, fmt.Errorf("no organizations on this account")
	}
	if len(orgs) == 1 {
		return orgs[0].ID, nil
	}
	options := make([]huh.Option[int64], 0, len(orgs))
	for _, o := range orgs {
		options = append(options, huh.NewOption(fmt.Sprintf("%s  (%s)", o.Name, o.Slug), o.ID))
	}
	var picked int64
	err = huh.NewSelect[int64]().
		Title("Pick an organization").
		Description("Type / to filter.").
		Filtering(true).
		Options(options...).
		Value(&picked).
		Run()
	if err != nil {
		return 0, fmt.Errorf("quick_book: %w", err)
	}
	return picked, nil
}

func pickLocation(c *robin.Client, orgID, current int64) (int64, error) {
	locs, err := c.Locations(orgID)
	if err != nil {
		return 0, err
	}
	if len(locs) == 0 {
		return 0, fmt.Errorf("no locations in this organization")
	}
	options := make([]huh.Option[int64], 0, len(locs))
	for _, l := range locs {
		label := fmt.Sprintf("%s  (%s)", l.Name, l.Slug)
		if l.ID == current {
			label += "  ← current"
		}
		options = append(options, huh.NewOption(label, l.ID))
	}
	picked := current
	if picked == 0 {
		picked = locs[0].ID
	}
	err = huh.NewSelect[int64]().
		Title("Pick a default location for auto-pick").
		Description("Type / to filter.").
		Filtering(true).
		Options(options...).
		Value(&picked).
		Run()
	if err != nil {
		return 0, fmt.Errorf("quick_book: %w", err)
	}
	return picked, nil
}

// pickPriorityRooms walks the user through an ordered priority list. Rooms
// must match the "Meeting Room <N>" naming convention robin's auto-pick code
// uses. Sequential single-select with a "✓ Done" sentinel preserves order.
func pickPriorityRooms(c *robin.Client, locationID int64, current []int) ([]int, error) {
	spaces, err := c.Spaces(locationID)
	if err != nil {
		return nil, err
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
	if len(byNum) == 0 {
		return nil, fmt.Errorf("no rooms named 'Meeting Room <N>' in this location — priority list left empty")
	}

	if len(current) > 0 {
		startOver := false
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Replace current priority list (%d rooms)?", len(current))).
			Description(formatCurrentPriority(current, byNum)).
			Affirmative("Yes, start over").
			Negative("Keep current").
			Value(&startOver).
			Run(); err != nil {
			return nil, fmt.Errorf("quick_book: %w", err)
		}
		if !startOver {
			return current, nil
		}
	}

	const doneSentinel = -1
	pickedSet := map[int]bool{}
	picked := []int{}
	for {
		var keys []int
		for k := range byNum {
			if !pickedSet[k] {
				keys = append(keys, k)
			}
		}
		sort.Ints(keys)

		options := []huh.Option[int]{huh.NewOption("✓ Done", doneSentinel)}
		for _, k := range keys {
			s := byNum[k]
			label := fmt.Sprintf("Meeting Room %d", k)
			if s.Capacity > 0 {
				label += fmt.Sprintf("  (capacity %d)", s.Capacity)
			}
			options = append(options, huh.NewOption(label, k))
		}

		title := fmt.Sprintf("Pick priority #%d", len(picked)+1)
		desc := "✓ Done if you're finished."
		if len(picked) > 0 {
			desc = "Picked so far: " + formatPickedRooms(picked)
		}

		choice := doneSentinel
		if len(keys) > 0 {
			choice = keys[0]
		}
		err := huh.NewSelect[int]().
			Title(title).
			Description(desc + "  Type / to filter.").
			Filtering(true).
			Options(options...).
			Value(&choice).
			Run()
		if err != nil {
			return nil, fmt.Errorf("quick_book: %w", err)
		}
		if choice == doneSentinel {
			return picked, nil
		}
		picked = append(picked, choice)
		pickedSet[choice] = true
		if len(pickedSet) == len(byNum) {
			return picked, nil
		}
	}
}

func formatCurrentPriority(nums []int, byNum map[int]robin.Space) string {
	parts := make([]string, 0, len(nums))
	for i, n := range nums {
		if _, ok := byNum[n]; ok {
			parts = append(parts, fmt.Sprintf("%d. Meeting Room %d", i+1, n))
		} else {
			parts = append(parts, fmt.Sprintf("%d. Meeting Room %d (no longer in this location)", i+1, n))
		}
	}
	return strings.Join(parts, "\n")
}

func formatPickedRooms(nums []int) string {
	parts := make([]string, 0, len(nums))
	for _, n := range nums {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ", ")
}

func pickTimeZone(current string) (string, error) {
	const customSentinel = "__custom__"
	options := make([]huh.Option[string], 0, len(commonTimeZones)+1)
	seen := map[string]bool{}
	if current != "" && !contains(commonTimeZones[0], "") {
		// Show the current value at the top if it isn't in the list, so the
		// user can keep it without retyping.
		found := false
		for _, tz := range commonTimeZones {
			if tz == current {
				found = true
				break
			}
		}
		if !found {
			options = append(options, huh.NewOption(current+"  ← current", current))
			seen[current] = true
		}
	}
	for _, tz := range commonTimeZones {
		if seen[tz] {
			continue
		}
		options = append(options, huh.NewOption(tz, tz))
		seen[tz] = true
	}
	options = append(options, huh.NewOption("Custom...", customSentinel))

	picked := current
	if picked == "" {
		picked = "Europe/Oslo"
	}
	err := huh.NewSelect[string]().
		Title("Default time zone").
		Description("Used when --time-zone isn't passed to robin book. Type / to filter.").
		Filtering(true).
		Options(options...).
		Value(&picked).
		Run()
	if err != nil {
		return "", fmt.Errorf("quick_book: %w", err)
	}
	if picked != customSentinel {
		return picked, nil
	}
	custom := current
	err = huh.NewInput().
		Title("Enter IANA time zone").
		Description("e.g. Pacific/Auckland, Asia/Tokyo").
		Value(&custom).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("required")
			}
			return nil
		}).
		Run()
	if err != nil {
		return "", fmt.Errorf("quick_book: %w", err)
	}
	return strings.TrimSpace(custom), nil
}

func pickDurations(curMin, curMax, curWindow int) (int, int, int, error) {
	minStr := strconv.Itoa(coalesceDefault(curMin, 30))
	maxStr := strconv.Itoa(coalesceDefault(curMax, 120))
	winStr := strconv.Itoa(coalesceDefault(curWindow, 30))

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Minimum slot length (minutes)").
				Description("Auto-pick rejects rooms with less free time than this.").
				Value(&minStr).
				Validate(validatePositiveMinutes(5)),
			huh.NewInput().
				Title("Maximum booking length (minutes)").
				Description("Cap on how long auto-pick will book.").
				Value(&maxStr).
				Validate(validatePositiveMinutes(5)),
			huh.NewInput().
				Title("Search window (minutes)").
				Description("How far past --start auto-pick is willing to look.").
				Value(&winStr).
				Validate(validatePositiveMinutes(0)),
		),
	)
	if err := form.Run(); err != nil {
		return 0, 0, 0, fmt.Errorf("quick_book: %w", err)
	}
	min, _ := strconv.Atoi(minStr)
	max, _ := strconv.Atoi(maxStr)
	window, _ := strconv.Atoi(winStr)
	if max < min {
		return 0, 0, 0, fmt.Errorf("max (%d) must be ≥ min (%d)", max, min)
	}
	return min, max, window, nil
}

func validatePositiveMinutes(min int) func(string) error {
	return func(s string) error {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("not a number")
		}
		if n < min {
			return fmt.Errorf("must be ≥ %d", min)
		}
		return nil
	}
}

func coalesceDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func pickWorkingHours(current *config.WorkingHours) (*config.WorkingHours, error) {
	startStr := "09:00"
	endStr := "17:00"
	defaultEnable := false
	if current != nil {
		if current.Start != "" {
			startStr = current.Start
		}
		if current.End != "" {
			endStr = current.End
		}
		defaultEnable = true
	}

	enable := defaultEnable
	title := "Set working hours?"
	if defaultEnable {
		title = fmt.Sprintf("Update working hours? (currently %s–%s)", current.Start, current.End)
	}
	if err := huh.NewConfirm().
		Title(title).
		Description("Auto-pick snaps a --start that lands before working hours up to the start of the workday — so 'robin book --start tomorrow' lands at the workday start.").
		Affirmative("Yes").
		Negative("Skip").
		Value(&enable).
		Run(); err != nil {
		return current, fmt.Errorf("quick_book: %w", err)
	}
	if !enable {
		if defaultEnable {
			return current, nil
		}
		return nil, nil
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Working hours start (HH:MM)").
				Value(&startStr).
				Validate(validateClockTime),
			huh.NewInput().
				Title("Working hours end (HH:MM)").
				Value(&endStr).
				Validate(validateClockTime),
		),
	)
	if err := form.Run(); err != nil {
		return current, fmt.Errorf("quick_book: %w", err)
	}
	startStr = strings.TrimSpace(startStr)
	endStr = strings.TrimSpace(endStr)
	wh := &config.WorkingHours{Start: startStr, End: endStr}
	// Sanity-check the pair through Snap on a synthetic time.
	if _, err := wh.Snap(time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		return current, err
	}
	return wh, nil
}

func validateClockTime(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("required")
	}
	_, _, err := config.ParseClockTime(s)
	return err
}

func pickTitle(current string) (string, error) {
	out := current
	err := huh.NewInput().
		Title("Default event title").
		Description("Optional. Leave empty to let Robin auto-generate one.").
		Value(&out).
		Run()
	if err != nil {
		return "", fmt.Errorf("quick_book: %w", err)
	}
	return strings.TrimSpace(out), nil
}
