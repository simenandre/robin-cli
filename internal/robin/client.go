package robin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	apiBase   = "https://api.robinpowered.com/v1.0"
	userAgent = "robin-cli/0.1 (+https://github.com/simenandre/robin-cli)"
)

type Client struct {
	http    *http.Client
	session *Session
	Verbose bool
}

func New() *Client {
	return &Client{http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) WithSession(s *Session) *Client {
	c.session = s
	return c
}

func (c *Client) Session() *Session { return c.session }

func (c *Client) logf(format string, args ...any) {
	if !c.Verbose {
		return
	}
	fmt.Fprintf(os.Stderr, "[robin] "+format+"\n", args...)
}

type apiEnvelope struct {
	Meta struct {
		StatusCode int    `json:"status_code"`
		Status     string `json:"status"`
		Message    string `json:"message"`
		Errors     []any  `json:"errors"`
		MoreInfo   any    `json:"more_info"`
	} `json:"meta"`
	Data json.RawMessage `json:"data"`
}

// Login exchanges email/password for an access token via the Robin core API.
// Mirrors the dashboard's Auth.login: POST /auth/users with Basic auth and a
// JSON body of {remember_me, organization}. organization may be empty.
func (c *Client) Login(email, password string, organization *int) (*Session, error) {
	body, _ := json.Marshal(map[string]any{
		"remember_me":  false,
		"organization": organization,
	})
	req, err := http.NewRequest(http.MethodPost, apiBase+"/auth/users", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	creds := base64.StdEncoding.EncodeToString([]byte(email + ":" + password))
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	c.logf("POST %s/auth/users (Basic auth, email=%q)", apiBase, email)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	c.logf("  -> %s", resp.Status)

	var env apiEnvelope
	if err := json.Unmarshal(rb, &env); err != nil {
		return nil, fmt.Errorf("login: parse response (%d): %w (body: %s)", resp.StatusCode, err, truncate(string(rb), 300))
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		msg := env.Meta.Message
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("login failed (%d): %s", resp.StatusCode, msg)
	}

	var d struct {
		AccessToken string `json:"access_token"`
		ExpireAt    string `json:"expire_at"`
		AccountID   int64  `json:"account_id"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		return nil, fmt.Errorf("login: parse data: %w (body: %s)", err, truncate(string(rb), 300))
	}
	s := &Session{AccessToken: d.AccessToken, AccountID: d.AccountID}
	if d.ExpireAt != "" {
		// Robin returns RFC3339-ish; tolerate parse failure
		if t, perr := time.Parse(time.RFC3339, d.ExpireAt); perr == nil {
			s.ExpiresAt = t
		}
	}
	c.session = s
	return s, nil
}

// Get does an authenticated GET against the core API and decodes the data field
// into out (which may be nil to discard).
func (c *Client) Get(pathAndQuery string, out any) error {
	return c.do(http.MethodGet, pathAndQuery, nil, out)
}

func (c *Client) Post(pathAndQuery string, body, out any) error {
	return c.do(http.MethodPost, pathAndQuery, body, out)
}

func (c *Client) do(method, pathAndQuery string, body, out any) error {
	if c.session == nil || c.session.AccessToken == "" {
		return fmt.Errorf("not authenticated — run `robin login` first")
	}
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, apiBase+pathAndQuery, buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Access-Token "+c.session.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.logf("%s %s%s", method, apiBase, pathAndQuery)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	c.logf("  -> %s", resp.Status)

	if resp.StatusCode >= 400 {
		c.logf("  raw body: %s", truncate(string(rb), 800))
		var env apiEnvelope
		_ = json.Unmarshal(rb, &env)
		msg := env.Meta.Message
		if len(env.Meta.Errors) > 0 {
			eb, _ := json.Marshal(env.Meta.Errors)
			msg = msg + " | errors: " + string(eb)
		}
		if msg == "" {
			msg = truncate(string(rb), 500)
		}
		return fmt.Errorf("%s %s: %d %s", method, pathAndQuery, resp.StatusCode, msg)
	}
	if out == nil {
		return nil
	}
	var env apiEnvelope
	if err := json.Unmarshal(rb, &env); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	return json.Unmarshal(env.Data, out)
}

type Me struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	PrimaryEmail struct {
		Email string `json:"email"`
	} `json:"primary_email"`
	Slug     string `json:"slug"`
	TimeZone string `json:"time_zone"`
}

func (c *Client) Whoami() (*Me, error) {
	var m Me
	if err := c.Get("/me", &m); err != nil {
		return nil, err
	}
	return &m, nil
}

type Organization struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (c *Client) MyOrganizations() ([]Organization, error) {
	var orgs []Organization
	if err := c.Get("/me/organizations", &orgs); err != nil {
		return nil, err
	}
	return orgs, nil
}

type Location struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (c *Client) Locations(orgID int64) ([]Location, error) {
	var locs []Location
	if err := c.Get(fmt.Sprintf("/organizations/%d/locations", orgID), &locs); err != nil {
		return nil, err
	}
	return locs, nil
}

type Space struct {
	ID         int64  `json:"id"`
	LocationID int64  `json:"location_id"`
	LevelID    int64  `json:"level_id"`
	Name       string `json:"name"`
	Capacity   int    `json:"capacity"`
	Type       string `json:"type"`
	IsDisabled bool   `json:"is_disabled"`
}

func (c *Client) Spaces(locationID int64) ([]Space, error) {
	var spaces []Space
	if err := c.Get(fmt.Sprintf("/locations/%d/spaces?per_page=200", locationID), &spaces); err != nil {
		return nil, err
	}
	return spaces, nil
}

type DateTime struct {
	DateTime string `json:"date_time"`
	TimeZone string `json:"time_zone"`
}

func (dt DateTime) Time() (time.Time, error) {
	if dt.DateTime == "" {
		return time.Time{}, fmt.Errorf("empty date_time")
	}
	for _, layout := range []string{
		time.RFC3339,                  // 2026-04-28T10:00:00+02:00
		"2006-01-02T15:04:05-0700",    // 2026-04-28T10:00:00+0200 (Robin's format)
		"2006-01-02T15:04:05.000-0700",
	} {
		if t, err := time.Parse(layout, dt.DateTime); err == nil {
			return t, nil
		}
	}
	loc := time.UTC
	if dt.TimeZone != "" {
		if l, err := time.LoadLocation(dt.TimeZone); err == nil {
			loc = l
		}
	}
	return time.ParseInLocation("2006-01-02T15:04:05", dt.DateTime, loc)
}

type BookRequest struct {
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Start       DateTime `json:"start"`
	End         DateTime `json:"end"`
	Visibility  string   `json:"visibility,omitempty"`
}

type Event struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Start        DateTime      `json:"start"`
	End          DateTime      `json:"end"`
	SpaceID      int64         `json:"space_id,omitempty"`
	Confirmation *Confirmation `json:"confirmation,omitempty"`
}

// Confirmation reports whether an event has been checked in. Robin returns
// the field as null for unconfirmed events.
type Confirmation struct {
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	UserID      int64      `json:"user_id,omitempty"`
}

// IsConfirmed reports whether someone has already checked into the event.
func (e Event) IsConfirmed() bool {
	return e.Confirmation != nil && e.Confirmation.ConfirmedAt != nil && !e.Confirmation.ConfirmedAt.IsZero()
}

func (c *Client) BookSpace(spaceID int64, req BookRequest) (*Event, error) {
	var ev Event
	if err := c.Post(fmt.Sprintf("/spaces/%d/events", spaceID), req, &ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

func (c *Client) SpaceEvents(spaceID int64, after, before time.Time) ([]Event, error) {
	q := url.Values{}
	q.Set("after", after.UTC().Format(time.RFC3339))
	q.Set("before", before.UTC().Format(time.RFC3339))
	q.Set("per_page", "100")
	var events []Event
	if err := c.Get(fmt.Sprintf("/spaces/%d/events?%s", spaceID, q.Encode()), &events); err != nil {
		return nil, err
	}
	return events, nil
}

// MyEvents returns events the current user is part of in [after, before].
func (c *Client) MyEvents(after, before time.Time) ([]Event, error) {
	q := url.Values{}
	q.Set("after", after.UTC().Format(time.RFC3339))
	q.Set("before", before.UTC().Format(time.RFC3339))
	q.Set("per_page", "100")
	var events []Event
	if err := c.Get("/me/events?"+q.Encode(), &events); err != nil {
		return nil, err
	}
	return events, nil
}

// ConfirmEvent marks an event as checked-in. Robin's check-in window is
// server-side: this can return 4xx for events that are too early, too late,
// or already confirmed.
func (c *Client) ConfirmEvent(eventID string) error {
	return c.Post(fmt.Sprintf("/events/%s/confirmation", eventID), nil, nil)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}
