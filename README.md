# robin

CLI to book [Robin](https://robinpowered.com) meeting rooms from your terminal.

Designed for users who don't have admin-issued API tokens. Authenticates
with your normal Robin email/password and uses the same `/auth/users`
endpoint the web dashboard uses internally.

## Install

```sh
go install github.com/simenandre/robin-user-api/cmd/robin@latest
```

Requires Go 1.26+.

## Setup

```sh
robin init    # prompts for org slug, email, password
robin login   # exchanges credentials for an access token
robin whoami  # prints the current user (sanity check)
```

Credentials and the access token are stored under `os.UserConfigDir()`
(macOS: `~/Library/Application Support/robin/`, Linux: `~/.config/robin/`),
mode `0600`.

## Commands

```
robin orgs                           list organizations
robin locations                      list locations in your org
robin spaces [--location ID]         list bookable spaces
robin book --space ID --start TIME   book a specific space
           [--end TIME | --duration 30m]
           [--title "..."] [--time-zone Europe/Oslo]
           [--yes]
robin now [--when WHEN] [--min N] [--max N] [--window N]
          [--prioritize-length] [--dry-run]
```

`-v` / `--verbose` (after the subcommand) dumps the HTTP exchange to stderr.

### `robin now` — quick book a meeting room

Books the best meeting room based on your priority list and current
availability. By default it:

1. Checks each room in your priority list for availability.
2. Picks the one with the **longest free slot**, capped at
   `max_duration_minutes` (default 2h). Priority order breaks ties.
3. Falls back to other meeting rooms in the location if no priority
   room is available.

Flags:

| flag | meaning |
|---|---|
| `--when` | Start search from a different time. Accepts a duration (`1h`, `30m`, `2h30m`), a clock time today (`14:00`), or a full datetime (`2026-04-29 09:00`, RFC3339). Default: now. |
| `--min N` | Minimum acceptable slot length in minutes. Skips rooms below this. |
| `--max N` | Maximum slot length in minutes. Caps the booking duration. |
| `--window N` | How many minutes after `--when` to search for an open slot. |
| `--prioritize-length` | Rank all meeting rooms by available length, ignoring priority order. Priority is then only a tiebreaker. |
| `--dry-run` | Find the best room and print it; don't book. |
| `--title` | Event title (otherwise Robin auto-generates one). |

Examples:

```sh
robin now                    # book best priority room starting now
robin now --when 1h          # book starting one hour from now
robin now --when 14:00       # book starting at 14:00 today
robin now --when "2026-04-29 09:00"
robin now --prioritize-length --max 60   # longest room ≤ 60 min
robin now --dry-run          # see what would be booked
```

## Config

`robin init` writes a starter `config.json`. To enable `robin now`, add a
`quick_book` block:

```json
{
  "org": "your-org-slug",
  "email": "you@example.com",
  "password": "...",
  "quick_book": {
    "location": 22847,
    "priority": [6, 7, 5, 4, 11, 10, 8, 9],
    "min_duration_minutes": 30,
    "max_duration_minutes": 120,
    "window_minutes": 30,
    "time_zone": "Europe/Oslo"
  }
}
```

| field | meaning |
|---|---|
| `location` | The location ID to search in (`robin locations` shows yours). |
| `priority` | Room numbers in preference order. The matcher looks for `Meeting Room N` in the space name. |
| `min_duration_minutes` | Minimum slot length to consider a room usable. |
| `max_duration_minutes` | Cap on booking length. |
| `window_minutes` | How long after the search anchor (`--when` or now) a slot may start. |
| `time_zone` | IANA timezone used when sending start/end to Robin. |
| `title` | Optional default event title. |

## How auth works

Robin's [public API](https://docs.robinpowered.com) requires admin-issued
access tokens. This tool instead replays the dashboard's own login flow:

1. `POST https://api.robinpowered.com/v1.0/auth/users` with
   `Authorization: Basic base64(email:password)` and a JSON body
   `{remember_me: false, organization: null}`.
2. The response yields `{access_token, expire_at, account_id}`.
3. Subsequent calls use `Authorization: Access-Token <token>`.

The token is cached in `session.json` next to the config file.
