package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/simenandre/robin-cli/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd(io *IO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and edit robin's local config.",
		Long: `Manage the credentials and quick_book settings stored at:

  ` + configFilePath() + `

Subcommands let you view the file (with the password masked by default),
edit your priority room list, or set working hours.`,
		Example: `  robin config show
  robin config priority
  robin config working-hours`,
	}
	cmd.AddCommand(
		newConfigShowCmd(io),
		newConfigGetCmd(io),
		newConfigSetCmd(io),
		newConfigPriorityCmd(io),
		newConfigWorkingHoursCmd(io),
	)
	return cmd
}

func newConfigShowCmd(io *IO) *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the current config (password masked by default).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if io.JSON {
				out := *cfg
				if !reveal {
					out.Password = "***"
				}
				return io.JSONOut(out)
			}
			printConfig(io, cfg, reveal)
			return nil
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal", false, "show the password in plaintext")
	return cmd
}

func newConfigPriorityCmd(io *IO) *cobra.Command {
	return &cobra.Command{
		Use:   "priority",
		Short: "Edit the auto-pick priority list of meeting rooms.",
		Long: `Walk through the spaces in your configured quick_book.location
and pick (in order) which rooms 'robin book' should prefer when more
than one is free.

Requires that you've run 'robin init' (to set the location) and
'robin login' (so the CLI can list spaces). Only rooms named
'Meeting Room <N>' are pickable — that's the convention robin's
auto-pick code uses to map priority numbers to spaces.`,
		Example: `  robin config priority`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if io.NoInput {
				return fmt.Errorf("config priority requires interactive input; cannot run with --no-input")
			}
			if !io.StdinTTY() {
				return fmt.Errorf("config priority requires a terminal")
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.QuickBook == nil || cfg.QuickBook.Location == 0 {
				return fmt.Errorf("quick_book.location is not set; run %s first", boldCmd("robin init"))
			}
			c, err := authedClient(io)
			if err != nil {
				return err
			}
			updated, err := pickPriorityRooms(c, cfg.QuickBook.Location, cfg.QuickBook.Priority)
			if err != nil {
				return err
			}
			cfg.QuickBook.Priority = updated
			if err := config.Save(cfg); err != nil {
				return err
			}
			io.Success("priority list saved (%d rooms)", len(updated))
			return nil
		},
	}
}

func newConfigWorkingHoursCmd(io *IO) *cobra.Command {
	return &cobra.Command{
		Use:   "working-hours",
		Short: "Set or update the working-hours bracket auto-pick uses to snap --start.",
		Long: `Working hours are a HH:MM start / HH:MM end pair. When 'robin book'
auto-picks and --start lands before working_hours.start, robin snaps
the search up to working_hours.start so 'robin book --start tomorrow'
finds a slot inside the workday.`,
		Example: `  robin config working-hours`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if io.NoInput {
				return fmt.Errorf("config working-hours requires interactive input; cannot run with --no-input")
			}
			if !io.StdinTTY() {
				return fmt.Errorf("config working-hours requires a terminal")
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.QuickBook == nil {
				cfg.QuickBook = &config.QuickBookConfig{}
			}
			wh, err := pickWorkingHours(cfg.QuickBook.WorkingHours)
			if err != nil {
				return err
			}
			cfg.QuickBook.WorkingHours = wh
			if err := config.Save(cfg); err != nil {
				return err
			}
			if wh == nil {
				io.Success("working hours cleared")
			} else {
				io.Success("working hours saved (%s–%s)", wh.Start, wh.End)
			}
			return nil
		},
	}
}

func printConfig(io *IO, cfg *config.Config, reveal bool) {
	pw := "***  (use --reveal to show)"
	if reveal {
		pw = cfg.Password
	}
	fmt.Fprintf(io.Out, "config: %s\n\n", configFilePath())
	tw := io.Tabular()
	fmt.Fprintf(tw, "  org:\t%s\n", cfg.Org)
	fmt.Fprintf(tw, "  email:\t%s\n", cfg.Email)
	fmt.Fprintf(tw, "  password:\t%s\n", pw)
	tw.Flush()

	if cfg.QuickBook == nil {
		fmt.Fprintf(io.Out, "\nquick_book: (not configured — run %s)\n", boldCmd("robin init"))
		return
	}
	qb := cfg.QuickBook
	fmt.Fprintf(io.Out, "\nquick_book:\n")
	tw = io.Tabular()
	fmt.Fprintf(tw, "  location:\t%d\n", qb.Location)
	fmt.Fprintf(tw, "  priority:\t%s\n", formatPriorityList(qb.Priority))
	fmt.Fprintf(tw, "  min duration:\t%s\n", formatMinutes(qb.MinDurationMinutes, 30))
	fmt.Fprintf(tw, "  max duration:\t%s\n", formatMinutes(qb.MaxDurationMinutes, 120))
	fmt.Fprintf(tw, "  search window:\t%s\n", formatMinutes(qb.WindowMinutes, 30))
	fmt.Fprintf(tw, "  time zone:\t%s\n", orDash(qb.TimeZone))
	fmt.Fprintf(tw, "  default title:\t%s\n", orDash(qb.Title))
	fmt.Fprintf(tw, "  working hours:\t%s\n", formatWorkingHours(qb.WorkingHours))
	tw.Flush()
}

func formatPriorityList(p []int) string {
	if len(p) == 0 {
		return "—"
	}
	parts := make([]string, len(p))
	for i, n := range p {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

func formatMinutes(v, fallback int) string {
	if v == 0 {
		return fmt.Sprintf("%d min  (default)", fallback)
	}
	return fmt.Sprintf("%d min", v)
}

func formatWorkingHours(w *config.WorkingHours) string {
	if w == nil || w.Start == "" || w.End == "" {
		return "—"
	}
	return w.Start + "–" + w.End
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func newConfigGetCmd(io *IO) *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Print a single config value.",
		Long: `Print one config field by dotted key. The password is masked
unless --reveal is passed. Run 'robin config show' to see every key.`,
		Example: `  robin config get org
  robin config get quick_book.location
  robin config get quick_book.priority`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			v, err := getConfigValue(cfg, args[0], reveal)
			if err != nil {
				return err
			}
			io.Println(v)
			return nil
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal", false, "show the password in plaintext")
	return cmd
}

func newConfigSetCmd(io *IO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a single config value.",
		Long: `Set a config field by dotted key. Use 'robin config priority'
or 'robin config working-hours' for the interactive multi-step pickers.

Supported keys:
  org
  email
  password
  quick_book.location              (integer)
  quick_book.title
  quick_book.time_zone             (IANA, e.g. Europe/Oslo)
  quick_book.min_duration_minutes  (integer)
  quick_book.max_duration_minutes  (integer)
  quick_book.window_minutes        (integer)
  quick_book.priority              (comma-separated ints, "" to clear)
  quick_book.working_hours.start   (HH:MM, "" to clear)
  quick_book.working_hours.end     (HH:MM, "" to clear)`,
		Example: `  robin config set org acme
  robin config set quick_book.location 172344
  robin config set quick_book.priority "4,7,12"
  robin config set quick_book.working_hours.start 09:00`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := setConfigValue(cfg, args[0], args[1]); err != nil {
				return err
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			io.Success("set %s", args[0])
			return nil
		},
	}
	return cmd
}

func getConfigValue(cfg *config.Config, key string, reveal bool) (string, error) {
	qb := cfg.QuickBook
	switch key {
	case "org":
		return cfg.Org, nil
	case "email":
		return cfg.Email, nil
	case "password":
		if reveal {
			return cfg.Password, nil
		}
		return "***", nil
	case "quick_book.location":
		if qb == nil {
			return "0", nil
		}
		return strconv.FormatInt(qb.Location, 10), nil
	case "quick_book.title":
		if qb == nil {
			return "", nil
		}
		return qb.Title, nil
	case "quick_book.time_zone":
		if qb == nil {
			return "", nil
		}
		return qb.TimeZone, nil
	case "quick_book.min_duration_minutes":
		if qb == nil {
			return "0", nil
		}
		return strconv.Itoa(qb.MinDurationMinutes), nil
	case "quick_book.max_duration_minutes":
		if qb == nil {
			return "0", nil
		}
		return strconv.Itoa(qb.MaxDurationMinutes), nil
	case "quick_book.window_minutes":
		if qb == nil {
			return "0", nil
		}
		return strconv.Itoa(qb.WindowMinutes), nil
	case "quick_book.priority":
		if qb == nil {
			return "", nil
		}
		return formatPriorityCSV(qb.Priority), nil
	case "quick_book.working_hours.start":
		if qb == nil || qb.WorkingHours == nil {
			return "", nil
		}
		return qb.WorkingHours.Start, nil
	case "quick_book.working_hours.end":
		if qb == nil || qb.WorkingHours == nil {
			return "", nil
		}
		return qb.WorkingHours.End, nil
	}
	return "", fmt.Errorf("unknown key %q", key)
}

func setConfigValue(cfg *config.Config, key, value string) error {
	switch key {
	case "org":
		cfg.Org = value
	case "email":
		cfg.Email = value
	case "password":
		cfg.Password = value
	case "quick_book.location":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("location must be an integer, got %q", value)
		}
		ensureQuickBook(cfg).Location = n
	case "quick_book.title":
		ensureQuickBook(cfg).Title = value
	case "quick_book.time_zone":
		if value != "" {
			if _, err := time.LoadLocation(value); err != nil {
				return fmt.Errorf("invalid time zone %q: %w", value, err)
			}
		}
		ensureQuickBook(cfg).TimeZone = value
	case "quick_book.min_duration_minutes":
		n, err := parseNonNegativeInt(value)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		ensureQuickBook(cfg).MinDurationMinutes = n
	case "quick_book.max_duration_minutes":
		n, err := parseNonNegativeInt(value)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		ensureQuickBook(cfg).MaxDurationMinutes = n
	case "quick_book.window_minutes":
		n, err := parseNonNegativeInt(value)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		ensureQuickBook(cfg).WindowMinutes = n
	case "quick_book.priority":
		nums, err := parsePriorityCSV(value)
		if err != nil {
			return err
		}
		ensureQuickBook(cfg).Priority = nums
	case "quick_book.working_hours.start":
		return setWorkingHourSide(cfg, true, value)
	case "quick_book.working_hours.end":
		return setWorkingHourSide(cfg, false, value)
	default:
		return fmt.Errorf("unknown key %q (run %s)", key, boldCmd("robin config set --help"))
	}
	return nil
}

func ensureQuickBook(cfg *config.Config) *config.QuickBookConfig {
	if cfg.QuickBook == nil {
		cfg.QuickBook = &config.QuickBookConfig{}
	}
	return cfg.QuickBook
}

func setWorkingHourSide(cfg *config.Config, isStart bool, value string) error {
	if value != "" {
		if _, _, err := config.ParseClockTime(value); err != nil {
			return err
		}
	}
	qb := ensureQuickBook(cfg)
	if qb.WorkingHours == nil {
		qb.WorkingHours = &config.WorkingHours{}
	}
	if isStart {
		qb.WorkingHours.Start = value
	} else {
		qb.WorkingHours.End = value
	}
	if qb.WorkingHours.Start == "" && qb.WorkingHours.End == "" {
		qb.WorkingHours = nil
	}
	return nil
}

func parseNonNegativeInt(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("integer required, got %q", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("must be ≥ 0")
	}
	return n, nil
}

func parsePriorityCSV(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("priority must be comma-separated ints, got %q", s)
		}
		out = append(out, n)
	}
	return out, nil
}

func formatPriorityCSV(p []int) string {
	parts := make([]string, len(p))
	for i, n := range p {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}
