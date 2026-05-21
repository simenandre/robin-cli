package cli

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/simenandre/robin-cli/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCalendarsCmd(io *IO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendars",
		Short: "Manage iCal calendars that feed the book picker.",
		Long: `Robin reads each configured iCal feed to populate 'robin book'
with upcoming meetings. Pick an event in the book picker to attach a
room; the booking is stamped with the iCal UID so robin won't double-book
the same event next time.`,
		Example: `  robin config calendars add
  robin config calendars list
  robin config calendars remove`,
	}
	cmd.AddCommand(
		newConfigCalendarsListCmd(io),
		newConfigCalendarsAddCmd(io),
		newConfigCalendarsRemoveCmd(io),
	)
	return cmd
}

func newConfigCalendarsListCmd(io *IO) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print configured calendars.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cals := configuredCalendars(cfg)
			if io.JSON {
				return io.JSONOut(cals)
			}
			if len(cals) == 0 {
				io.Status("no calendars configured — run %s", boldCmd("robin config calendars add"))
				return nil
			}
			tw := io.Tabular()
			fmt.Fprintln(tw, "NAME\tURL")
			for _, c := range cals {
				fmt.Fprintf(tw, "%s\t%s\n", c.Name, c.URL)
			}
			return tw.Flush()
		},
	}
}

func newConfigCalendarsAddCmd(io *IO) *cobra.Command {
	var (
		name    string
		urlFlag string
	)
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add an iCal calendar to robin's config.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			ensureQuickBook(cfg)
			if name == "" || urlFlag == "" {
				if io.NoInput || !io.StdinTTY() {
					return fmt.Errorf("--name and --url are required with --no-input or no TTY")
				}
				form := huh.NewForm(huh.NewGroup(
					huh.NewInput().
						Title("Calendar name").
						Description("Short label, e.g. 'Work' or 'Personal'.").
						Value(&name).
						Validate(validateCalendarName(cfg.QuickBook.Calendars)),
					huh.NewInput().
						Title("iCal URL").
						Description("https:// or webcal://; usually a 'secret address' from Google/Outlook/iCloud.").
						Value(&urlFlag).
						Validate(validateCalendarURL),
				))
				if err := form.Run(); err != nil {
					return fmt.Errorf("calendars add: %w", err)
				}
			} else {
				if err := validateCalendarName(cfg.QuickBook.Calendars)(name); err != nil {
					return err
				}
				if err := validateCalendarURL(urlFlag); err != nil {
					return err
				}
			}
			cfg.QuickBook.Calendars = append(cfg.QuickBook.Calendars, config.CalendarSource{
				Name: strings.TrimSpace(name),
				URL:  strings.TrimSpace(urlFlag),
			})
			if err := config.Save(cfg); err != nil {
				return err
			}
			io.Success("added calendar %q", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "calendar label (interactive prompt if omitted)")
	cmd.Flags().StringVar(&urlFlag, "url", "", "iCal URL (interactive prompt if omitted)")
	return cmd
}

func newConfigCalendarsRemoveCmd(io *IO) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove an iCal calendar.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cals := configuredCalendars(cfg)
			if len(cals) == 0 {
				return fmt.Errorf("no calendars configured")
			}
			if name == "" {
				if io.NoInput || !io.StdinTTY() {
					return fmt.Errorf("--name is required with --no-input or no TTY")
				}
				options := make([]huh.Option[string], 0, len(cals))
				for _, c := range cals {
					options = append(options, huh.NewOption(c.Name+"  ("+c.URL+")", c.Name))
				}
				if err := huh.NewSelect[string]().
					Title("Pick a calendar to remove").
					Description("Type / to filter.").
					Filtering(true).
					Options(options...).
					Value(&name).
					Run(); err != nil {
					return fmt.Errorf("calendars remove: %w", err)
				}
			}
			out := make([]config.CalendarSource, 0, len(cals))
			removed := false
			for _, c := range cals {
				if c.Name == name {
					removed = true
					continue
				}
				out = append(out, c)
			}
			if !removed {
				return fmt.Errorf("no calendar named %q", name)
			}
			cfg.QuickBook.Calendars = out
			if err := config.Save(cfg); err != nil {
				return err
			}
			io.Success("removed calendar %q", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "calendar to remove (interactive picker if omitted)")
	return cmd
}

func configuredCalendars(cfg *config.Config) []config.CalendarSource {
	if cfg.QuickBook == nil {
		return nil
	}
	return cfg.QuickBook.Calendars
}

func validateCalendarName(existing []config.CalendarSource) func(string) error {
	return func(s string) error {
		s = strings.TrimSpace(s)
		if s == "" {
			return fmt.Errorf("required")
		}
		for _, c := range existing {
			if strings.EqualFold(c.Name, s) {
				return fmt.Errorf("a calendar named %q already exists", s)
			}
		}
		return nil
	}
}

func validateCalendarURL(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("required")
	}
	u, err := url.Parse(s)
	if err != nil {
		return err
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" && scheme != "webcal" {
		return fmt.Errorf("scheme must be http, https, or webcal; got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL missing host")
	}
	return nil
}
