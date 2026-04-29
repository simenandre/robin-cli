# robin

Book [Robin](https://robinpowered.com) meeting rooms from your terminal.

Designed for users who don't have admin-issued API tokens. Authenticates
with your normal Robin email/password using the same `/auth/users`
endpoint the web dashboard uses internally.

```sh
$ robin now
✓ booked Meeting Room 5 — Wed Apr 29 12:00 to 13:00 (1h0m0s)
```

## Install

```sh
go install github.com/simenandre/robin-user-api/cmd/robin@latest
```

Requires Go 1.26+.

## Quick start

```sh
robin init     # save org slug, email, password
robin login    # exchange them for a cached access token
robin now      # book the best priority room right now
```

Credentials and the access token live under your OS config directory
(macOS `~/Library/Application Support/robin/`, Linux `~/.config/robin/`),
mode `0600`. See [Config](#config) below to enable `robin now`.

## Commands

```
robin init                       save credentials to a config file
robin login                      authenticate, cache an access token
robin logout                     forget the cached token
robin whoami                     print the current user

robin orgs                       list organizations
robin locations                  list locations across orgs
robin spaces [-l LOCATION]       list bookable spaces

robin now [--when WHEN]          book the best priority room
robin book --space ID --start T  book a specific space
```

Every list command supports `--json` for machine-readable output. Every
command supports `-v` / `--verbose`, `-q` / `--quiet`, `--no-color`,
`--no-input`, `--help`. Run `robin help <command>` for the full surface
of any subcommand.

### `robin now` — quick book

Looks up `quick_book.priority` in your config, finds the room with the
**longest free slot** (capped at `max_duration_minutes`), and books it.
Falls back to other meeting rooms if no priority room is available.

```sh
robin now                       # book now (or up to 30 min from now)
robin now --dry-run             # see the pick without booking

# strict forms
robin now --when 1h             # 1 hour from now
robin now --when 14:00          # 14:00 today
robin now --when "2026-04-30 09:00"

# natural language (any of these work)
robin now --when tomorrow
robin now --when "tomorrow 9am"
robin now --when "in 2 hours"
robin now --when "monday 9am"          # upcoming Monday
robin now --when "next monday at 14:00"

robin now --max 60              # cap booking length at 60 min
robin now --prioritize-length   # ignore priority, take longest slot anywhere
```

`--when` accepts (in order of precedence):

1. `now` (or empty) — current time
2. Go duration: `1h`, `30m`, `2h30m`
3. Clock time today: `14:00`, `09:30`
4. Strict datetime: `2026-04-29 09:00`, `2026-04-29T09:00`, RFC3339
5. **Natural language** (English, future-direction): `tomorrow`,
   `tomorrow 9am`, `in 2 hours`, `monday 9am`, `next friday`, `9am`,
   `yesterday`. Powered by [`go-naturaldate`](https://github.com/tj/go-naturaldate);
   if a phrase is ambiguous prefer the explicit `2026-04-30 14:00` form.

| flag | meaning |
|---|---|
| `--when` | Search anchor. Duration (`1h`, `30m`), today's clock time (`14:00`), or full datetime. Default: now. |
| `--min N` | Minimum acceptable slot length (minutes). |
| `--max N` | Cap on booking length (minutes). |
| `--window N` | How far past the anchor a slot may start (minutes). |
| `--prioritize-length`, `-L` | Rank all rooms by length, ignoring priority order. |
| `-n`, `--dry-run` | Find the best room and print it; don't book. |
| `--title` | Event title (otherwise Robin auto-generates one). |

### `robin book` — book a specific space

```sh
robin book --space 172344 --start "2026-04-29 14:00" --duration 30m
robin book --space 172344 --start "14:00" --duration 1h --yes
robin book --space 172344 --start "2026-04-29T14:00:00+02:00" \
           --end   "2026-04-29T15:00:00+02:00" --title "Sync"
```

Robin requires events to be at least 5 minutes long. Times accept RFC3339
or local forms. Without `--yes`, prompts before posting.

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
| `location` | Location ID to search in (`robin locations` shows yours). |
| `priority` | Room numbers in preference order. The matcher looks for `Meeting Room N` in space names. |
| `min_duration_minutes` | Skip rooms with less free time than this. |
| `max_duration_minutes` | Cap on booking length. |
| `window_minutes` | How far past the search anchor a slot may start. |
| `time_zone` | IANA timezone used for booking start/end. |
| `title` | Optional default event title. |

## Output, color, and scripts

`robin` follows the [Command Line Interface Guidelines](https://clig.dev):

- **Human output by default**, formatted tables, color sparingly (errors
  red, success green). Auto-disables when not connected to a terminal,
  or when `NO_COLOR` is set, or with `--no-color`.
- **`--json`** on every command that prints data. Pipe-friendly and
  stable for scripts.
- **`--quiet`** suppresses non-essential output; exit code is the source
  of truth for success.
- **`--verbose`** shows the HTTP exchange and per-room availability.
- **`--no-input`** fails rather than prompting. Combine with `--yes` for
  CI / scripts that need `robin book`.
- **stdout** for results, **stderr** for status, progress, and errors.

Shell completion:

```sh
robin completion bash > /etc/bash_completion.d/robin
robin completion zsh  > "${fpath[1]}/_robin"
robin completion fish > ~/.config/fish/completions/robin.fish
```

## How auth works

Robin's [public API](https://docs.robinpowered.com) requires admin-issued
access tokens. This tool instead replays the dashboard's own login flow:

1. `POST https://api.robinpowered.com/v1.0/auth/users` with
   `Authorization: Basic base64(email:password)` and a JSON body
   `{remember_me: false, organization: null}`.
2. The response yields `{access_token, expire_at, account_id}`.
3. Subsequent calls use `Authorization: Access-Token <token>`.

The token is cached in `session.json` next to the config file. When it
expires, you'll see a 401 and a hint to run `robin login` again.
