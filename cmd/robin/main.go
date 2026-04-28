package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/simenandre/robin-user-api/internal/config"
	"github.com/simenandre/robin-user-api/internal/robin"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit()
	case "login":
		err = cmdLogin()
	case "whoami":
		err = cmdWhoami()
	case "orgs":
		err = cmdOrgs()
	case "locations":
		err = cmdLocations()
	case "spaces":
		err = cmdSpaces()
	case "book":
		err = cmdBook()
	case "now":
		err = cmdNow()
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `robin — book Robin meeting rooms via dashboard credentials

usage:
  robin init                              save credentials
  robin login                             authenticate, cache access token
  robin whoami                            print current user
  robin orgs                              list organizations
  robin locations                         list locations in your org
  robin spaces [--location ID]            list bookable spaces (omit --location for all)
  robin now [--when WHEN] [--min N] [--max N] [--window N]
            [--prioritize-length] [--dry-run]
                                          book a priority room; falls back to any meeting room
                                          if none available. --prioritize-length picks the
                                          longest-available room overall, ignoring priority.
                                          --when accepts '1h' / '30m' (relative), '14:00'
                                          (today), or '2026-04-29 09:00' (absolute).
  robin book --space ID --start TIME      book a space
        [--end TIME | --duration 30m]
        [--title "..."] [--description "..."]
        [--time-zone Europe/Oslo]
        [--yes]                          skip confirmation

times: RFC3339 (2026-04-29T09:00:00+02:00) or local "2026-04-29 09:00"
flags after subcommand: -v / --verbose dumps HTTP exchange`)
}

func loadAuthed() (*robin.Client, error) {
	sess, err := robin.LoadSession()
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, fmt.Errorf("no session — run `robin login` first")
	}
	c := robin.New().WithSession(sess)
	c.Verbose = hasFlag("-v") || hasFlag("--verbose")
	return c, nil
}

func hasFlag(name string) bool {
	for _, a := range os.Args[2:] {
		if a == name {
			return true
		}
	}
	return false
}

func cmdInit() error {
	in := bufio.NewReader(os.Stdin)
	org, err := prompt(in, "org slug (e.g. startuplab): ")
	if err != nil {
		return err
	}
	email, err := prompt(in, "email: ")
	if err != nil {
		return err
	}
	fmt.Print("password: ")
	pwBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return err
	}
	c := &config.Config{Org: org, Email: email, Password: string(pwBytes)}
	if err := config.Save(c); err != nil {
		return err
	}
	dir, _ := config.Dir()
	fmt.Printf("saved %s/config.json\n", dir)
	return nil
}

func cmdLogin() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client := robin.New()
	client.Verbose = hasFlag("-v") || hasFlag("--verbose")
	sess, err := client.Login(cfg.Email, cfg.Password, nil)
	if err != nil {
		return err
	}
	if err := robin.SaveSession(sess); err != nil {
		return err
	}
	fmt.Printf("logged in (account_id=%d)\n", sess.AccountID)
	return nil
}

func cmdWhoami() error {
	c, err := loadAuthed()
	if err != nil {
		return err
	}
	me, err := c.Whoami()
	if err != nil {
		return err
	}
	out, _ := json.MarshalIndent(me, "", "  ")
	fmt.Println(string(out))
	return nil
}

func cmdOrgs() error {
	c, err := loadAuthed()
	if err != nil {
		return err
	}
	orgs, err := c.MyOrganizations()
	if err != nil {
		return err
	}
	for _, o := range orgs {
		fmt.Printf("%-10d  %-30s  %s\n", o.ID, o.Slug, o.Name)
	}
	return nil
}

func cmdLocations() error {
	c, err := loadAuthed()
	if err != nil {
		return err
	}
	orgs, err := c.MyOrganizations()
	if err != nil {
		return err
	}
	for _, o := range orgs {
		locs, err := c.Locations(o.ID)
		if err != nil {
			return fmt.Errorf("locations for %s: %w", o.Slug, err)
		}
		for _, l := range locs {
			fmt.Printf("%-10d  %-30s  (org=%s)\n", l.ID, l.Name, o.Slug)
		}
	}
	return nil
}

func cmdSpaces() error {
	fs := flag.NewFlagSet("spaces", flag.ExitOnError)
	loc := fs.Int64("location", 0, "filter to a single location id")
	fs.Bool("verbose", false, "")
	fs.Bool("v", false, "")
	_ = fs.Parse(os.Args[2:])

	c, err := loadAuthed()
	if err != nil {
		return err
	}

	var locIDs []int64
	if *loc != 0 {
		locIDs = []int64{*loc}
	} else {
		orgs, err := c.MyOrganizations()
		if err != nil {
			return err
		}
		for _, o := range orgs {
			locs, err := c.Locations(o.ID)
			if err != nil {
				return err
			}
			for _, l := range locs {
				locIDs = append(locIDs, l.ID)
			}
		}
	}

	for _, id := range locIDs {
		spaces, err := c.Spaces(id)
		if err != nil {
			return fmt.Errorf("spaces for location %d: %w", id, err)
		}
		for _, s := range spaces {
			if s.IsDisabled {
				continue
			}
			fmt.Printf("%-10d  loc=%-7d  cap=%-3d  %-12s  %s\n", s.ID, s.LocationID, s.Capacity, s.Type, s.Name)
		}
	}
	return nil
}

func cmdBook() error {
	fs := flag.NewFlagSet("book", flag.ExitOnError)
	space := fs.Int64("space", 0, "space id (required)")
	startStr := fs.String("start", "", "start time (RFC3339 or 'YYYY-MM-DD HH:MM') (required)")
	endStr := fs.String("end", "", "end time (mutually exclusive with --duration)")
	durStr := fs.String("duration", "", "duration like 30m, 1h (mutually exclusive with --end)")
	tzStr := fs.String("time-zone", "Europe/Oslo", "IANA timezone for start/end")
	title := fs.String("title", "", "event title")
	desc := fs.String("description", "", "event description")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	fs.Bool("verbose", false, "")
	fs.Bool("v", false, "")
	_ = fs.Parse(os.Args[2:])

	if *space == 0 || *startStr == "" {
		fs.Usage()
		return fmt.Errorf("--space and --start are required")
	}
	if (*endStr == "") == (*durStr == "") {
		return fmt.Errorf("provide exactly one of --end or --duration")
	}

	tz, err := time.LoadLocation(*tzStr)
	if err != nil {
		return fmt.Errorf("invalid --time-zone: %w", err)
	}
	start, err := parseTime(*startStr, tz)
	if err != nil {
		return fmt.Errorf("invalid --start: %w", err)
	}
	var end time.Time
	if *endStr != "" {
		end, err = parseTime(*endStr, tz)
		if err != nil {
			return fmt.Errorf("invalid --end: %w", err)
		}
	} else {
		dur, err := time.ParseDuration(*durStr)
		if err != nil {
			return fmt.Errorf("invalid --duration: %w", err)
		}
		end = start.Add(dur)
	}
	if !end.After(start) {
		return fmt.Errorf("--end must be after --start")
	}
	if end.Sub(start) < 5*time.Minute {
		return fmt.Errorf("Robin requires events ≥ 5 minutes")
	}

	req := robin.BookRequest{
		Title:       *title,
		Description: *desc,
		Start:       robin.DateTime{DateTime: start.Format("2006-01-02T15:04:05"), TimeZone: *tzStr},
		End:         robin.DateTime{DateTime: end.Format("2006-01-02T15:04:05"), TimeZone: *tzStr},
	}

	fmt.Printf("about to book space %d:\n", *space)
	fmt.Printf("  title:       %s\n", orDefault(*title, "(auto)"))
	fmt.Printf("  start:       %s %s\n", req.Start.DateTime, req.Start.TimeZone)
	fmt.Printf("  end:         %s %s\n", req.End.DateTime, req.End.TimeZone)
	if *desc != "" {
		fmt.Printf("  description: %s\n", *desc)
	}
	if !*yes {
		fmt.Print("proceed? [y/N] ")
		in := bufio.NewReader(os.Stdin)
		ans, _ := in.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(ans)) != "y" {
			return fmt.Errorf("aborted")
		}
	}

	c, err := loadAuthed()
	if err != nil {
		return err
	}
	ev, err := c.BookSpace(*space, req)
	if err != nil {
		return err
	}
	out, _ := json.MarshalIndent(ev, "", "  ")
	fmt.Println(string(out))
	return nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func parseTime(s string, tz *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, tz); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse %q", s)
}

func prompt(r *bufio.Reader, label string) (string, error) {
	fmt.Print(label)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
