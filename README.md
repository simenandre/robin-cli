# robin

Book [Robin](https://robinpowered.com) meeting rooms from your terminal.

Designed for users who don't have admin-issued API tokens. Authenticates
with your normal Robin email/password using the same `/auth/users`
endpoint the web dashboard uses internally.

```sh
$ robin book
✓ booked Meeting Room 5 — Wed Apr 29 12:00 to 13:00 (1h0m0s)
```

## Install

```sh
go install github.com/simenandre/robin-cli/cmd/robin@latest
```

Requires Go 1.26+.

## Quick start

```sh
robin init     # save org slug, email, password
robin login    # exchange them for a cached access token
robin book     # book the best priority room right now
               # (also available as 'robin now')
```

Credentials and the access token live under your OS config directory
(macOS `~/Library/Application Support/robin/`, Linux `~/.config/robin/`),
mode `0600`. See [Config](#config) below to enable auto-pick.

## Commands

```
robin init                          save credentials to a config file
robin login / logout                manage the cached access token
robin whoami                        print the current user

robin orgs                          list organizations
robin locations                     list locations across orgs
robin spaces [-l LOCATION]          list bookable spaces

robin book                          book a meeting room (alias: robin now)
```

Every list command supports `--json` for machine-readable output. Every
command supports `-v` / `--verbose`, `-q` / `--quiet`, `--no-color`,
`--no-input`, `--help`. Run `robin help <command>` for the full surface
of any subcommand.

### `robin book` — book a meeting room

One command, two modes. Mode is picked by whether `--space` is set.

**Auto-pick** (no `--space`) — finds the best room based on your
`quick_book` config. Falls back to other meeting rooms if every priority
room is busy.

```sh
robin book                                # best room, starting now
robin book --start "tomorrow 9am"         # best room tomorrow at 9
robin book --start "in 2 hours"           # best room 2 hours from now
robin book --start now --duration 1h      # exactly 1h, starting now
robin book --max 60                       # cap booking length at 60 min
robin book --prioritize-length            # take longest slot anywhere
robin book --dry-run                      # see the pick without booking
```

`robin now` is a Cobra alias of `robin book` — same command, more
memorable for the daily-driver case.

**Specific** (`--space ID`) — books that exact room. Requires `--start`
plus either `--duration` or `--end`. Confirms before posting unless
`--yes`.

```sh
robin book --space 172344 --start "tomorrow 9am" --duration 30m
robin book --space 172344 --start "14:00" --duration 1h --yes
robin book --space 172344 \
           --start "2026-04-29T14:00:00+02:00" \
           --end   "2026-04-29T15:00:00+02:00" --title "Sync"
```

#### Time inputs

`--start`, `--end`, and `--duration` resolve in this order:

1. `now` or empty → current time
2. Go duration: `1h`, `30m`, `2h30m`
3. Clock time today: `14:00`, `09:30`
4. Strict datetime: `2026-04-29 09:00`, RFC3339
5. **Natural language** (future-direction): `tomorrow`, `tomorrow 9am`,
   `in 2 hours`, `monday 9am`, `next friday`, `9am`, `yesterday`.
   Powered by [`go-naturaldate`](https://github.com/tj/go-naturaldate);
   for ambiguous phrases use the explicit `2026-04-30 14:00` form.

#### Auto-pick algorithm

`--start` is the **earliest acceptable start**. The picker queries each
configured room (priority list first, then fallback meeting rooms), looks
for a free slot starting in `[--start, --start + window]` of at least
`--min` minutes. Among rooms with availability, the **longest slot**
wins (capped at `--max`); priority order breaks ties. With
`--prioritize-length`, all rooms compete equally on length and priority
is only a tiebreaker.

If `--duration` is set in auto-pick mode, the picker locks to that exact
length instead of maximizing.

#### Specific-mode flags

| flag | meaning |
|---|---|
| `--space`, `-s` | space ID to book |
| `--start` | exact start (required) |
| `--duration`, `-d` | event length (mutually exclusive with `--end`) |
| `--end` | exact end |
| `--yes`, `-y` | skip confirmation |
| `--title`, `--description` | event metadata |
| `--time-zone` | IANA timezone (default: from config, else `Europe/Oslo`) |

#### Auto-pick flags

| flag | meaning |
|---|---|
| `--start` | Earliest acceptable start (default: now). |
| `--duration`, `-d` | Lock booking to a fixed length instead of maximizing. |
| `--min N` | Minimum acceptable slot length (minutes). |
| `--max N` | Cap on booking length (minutes). |
| `--window N` | How far past `--start` a slot may start (minutes). |
| `--prioritize-length`, `-L` | Rank all rooms by length, ignoring priority order. |
| `--dry-run`, `-n` | Find the best room and print it; don't book. |

## Config

`robin init` writes a starter `config.json`. To enable auto-pick, add a
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
