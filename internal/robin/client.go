package robin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	apiBase   = "https://api.robinpowered.com/v1.0"
	userAgent = "robin-cli/0.1 (+https://github.com/simenandre/robin-cli)"
)

type Client struct {
	http       *http.Client
	session    *Session
	Verbose    bool
	authLoader func() (email, password string, err error)
}

func New() *Client {
	return &Client{http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) WithSession(s *Session) *Client {
	c.session = s
	return c
}

// SetAuth registers a callback the client can use to fetch credentials when
// it hits a 401. After a successful relogin the new session is saved to disk
// and the original request is retried once.
func (c *Client) SetAuth(loader func() (email, password string, err error)) {
	c.authLoader = loader
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
		StatusCode int              `json:"status_code"`
		Status     string           `json:"status"`
		Message    string           `json:"message"`
		Errors     []APIErrorDetail `json:"errors"`
		MoreInfo   map[string]any   `json:"more_info"`
	} `json:"meta"`
	Data json.RawMessage `json:"data"`
}

// APIError is what every Robin 4xx/5xx response decodes to. Use errors.As to
// inspect structured fields when handling errors from the client.
//
// Robin uses two side-channels for error detail:
//   - Errors[]: structured policy violations (e.g. booking_policy_violation
//     with details.max_length on too-long bookings).
//   - MoreInfo: a field→messages map used for validation errors like calendar
//     conflicts ("started_at and ended_at": ["overlaps event titled ..."]).
//
// The Error() string falls back through both so the user always sees the
// most actionable detail Robin returned.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Message    string
	Errors     []APIErrorDetail
	MoreInfo   map[string]any
}

// APIErrorDetail is one entry in apiEnvelope.Meta.Errors. Reason and Details
// carry the policy-specific information (e.g. booking_policy_violation with
// details.max_length when a booking exceeds the cap).
type APIErrorDetail struct {
	Domain  string         `json:"domain"`
	Reason  string         `json:"reason"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func (e *APIError) Error() string {
	base := fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.StatusCode, e.Message)
	if extra := e.detail(); extra != "" {
		return base + " — " + extra
	}
	return base
}

// detail returns a short human-readable summary of the structured error
// fields, preferring Errors[] (policy violations) then MoreInfo (field
// validation, e.g. calendar conflicts). Empty string when nothing useful.
func (e *APIError) detail() string {
	if len(e.Errors) > 0 {
		msgs := make([]string, 0, len(e.Errors))
		for _, d := range e.Errors {
			if d.Message != "" {
				msgs = append(msgs, d.Message)
			}
		}
		if len(msgs) > 0 {
			return strings.Join(msgs, "; ")
		}
		b, _ := json.Marshal(e.Errors)
		return "errors: " + string(b)
	}
	if len(e.MoreInfo) > 0 {
		var parts []string
		for field, v := range e.MoreInfo {
			switch tv := v.(type) {
			case []any:
				for _, m := range tv {
					parts = append(parts, fmt.Sprintf("%s: %v", field, m))
				}
			default:
				parts = append(parts, fmt.Sprintf("%s: %v", field, tv))
			}
		}
		return strings.Join(parts, "; ")
	}
	return ""
}

// TooLongBooking inspects the error details for the booking-length cap Robin
// reports as ISO-8601 PT duration (e.g. "PT2H"). Returns (cap, true) when
// the error tells us a max_length; (0, false) otherwise.
func (e *APIError) TooLongBooking() (time.Duration, bool) {
	for _, d := range e.Errors {
		v, ok := d.Details["max_length"].(string)
		if !ok {
			continue
		}
		if dur, err := parseISO8601PTDuration(v); err == nil && dur > 0 {
			return dur, true
		}
	}
	return 0, false
}

// AsAPIError unwraps err looking for a *APIError. Convenience wrapper around
// errors.As so call sites stay terse.
func AsAPIError(err error) (*APIError, bool) {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

// parseISO8601PTDuration parses the time-only ISO 8601 duration form Robin
// emits: PT1H30M, PT45S, PT2H. The date portion (P1DT...) is not supported.
func parseISO8601PTDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "PT") {
		return 0, fmt.Errorf("not a PT duration: %q", s)
	}
	rest := s[2:]
	if rest == "" {
		return 0, fmt.Errorf("empty PT duration: %q", s)
	}
	var total time.Duration
	var num strings.Builder
	for _, r := range rest {
		if r >= '0' && r <= '9' {
			num.WriteRune(r)
			continue
		}
		n, err := strconv.Atoi(num.String())
		if err != nil {
			return 0, fmt.Errorf("invalid number in %q", s)
		}
		num.Reset()
		switch r {
		case 'H':
			total += time.Duration(n) * time.Hour
		case 'M':
			total += time.Duration(n) * time.Minute
		case 'S':
			total += time.Duration(n) * time.Second
		default:
			return 0, fmt.Errorf("unknown unit %q in %q", r, s)
		}
	}
	if num.Len() > 0 {
		return 0, fmt.Errorf("trailing number without unit in %q", s)
	}
	return total, nil
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
	return c.doWithRetry(method, pathAndQuery, body, out, true)
}

func (c *Client) doWithRetry(method, pathAndQuery string, body, out any, allowRetry bool) error {
	err := c.doOnce(method, pathAndQuery, body, out)
	if err == nil {
		return nil
	}
	if !allowRetry || c.authLoader == nil {
		return err
	}
	apiErr, ok := AsAPIError(err)
	if !ok || apiErr.StatusCode != 401 {
		return err
	}
	c.logf("got 401, refreshing access token and retrying once")
	email, pw, lerr := c.authLoader()
	if lerr != nil {
		c.logf("auth loader failed: %v", lerr)
		return err
	}
	sess, lerr := c.Login(email, pw, nil)
	if lerr != nil {
		c.logf("relogin failed: %v", lerr)
		return err
	}
	if serr := SaveSession(sess); serr != nil {
		c.logf("warning: could not persist refreshed session: %v", serr)
	}
	c.session = sess
	return c.doWithRetry(method, pathAndQuery, body, out, false)
}

func (c *Client) doOnce(method, pathAndQuery string, body, out any) error {
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
		if msg == "" {
			msg = truncate(string(rb), 500)
		}
		return &APIError{
			Method:     method,
			Path:       pathAndQuery,
			StatusCode: resp.StatusCode,
			Message:    msg,
			Errors:     env.Meta.Errors,
			MoreInfo:   env.Meta.MoreInfo,
		}
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
	ConfirmedAt *FlexTime `json:"confirmed_at,omitempty"`
	UserID      int64     `json:"user_id,omitempty"`
}

// IsConfirmed reports whether someone has already checked into the event.
func (e Event) IsConfirmed() bool {
	return e.Confirmation != nil && e.Confirmation.ConfirmedAt != nil && !e.Confirmation.ConfirmedAt.IsZero()
}

// FlexTime tolerates the RFC3339 variants Robin emits — notably the
// "+0000" / "-0700" no-colon offset form, which Go's default time.Time
// UnmarshalJSON rejects. Used for any timestamp decoded from Robin's API.
type FlexTime struct {
	time.Time
}

var flexTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.000-0700",
	"2006-01-02T15:04:05-0700",
}

func (ft *FlexTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	var lastErr error
	for _, layout := range flexTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			ft.Time = t
			return nil
		} else {
			lastErr = err
		}
	}
	return fmt.Errorf("FlexTime: cannot parse %q: %w", s, lastErr)
}

func (ft FlexTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(ft.Time.Format(time.RFC3339Nano))
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

// DeleteEvent cancels an event. Robin returns 204 on success, 404 if the
// event doesn't exist, or 403 if the caller isn't allowed to delete it.
func (c *Client) DeleteEvent(eventID string) error {
	return c.do(http.MethodDelete, fmt.Sprintf("/events/%s", eventID), nil, nil)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}
