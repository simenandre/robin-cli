---
name: robin-cli
description: Book Robin meeting rooms from the terminal. Use when the user asks to find a free room, book a room "now", schedule one for "tomorrow 9am", check what spaces exist, or see which org/location they're configured against. The CLI authenticates with cached credentials so no admin API token is required.
allowed-tools: Bash(robin whoami*), Bash(robin orgs*), Bash(robin locations*), Bash(robin spaces*), Bash(robin book --dry-run*), Bash(robin book*), Bash(robin now*)
---

# robin CLI

## What robin is

robin books meeting rooms in [Robin](https://robinpowered.com/) from the
command line. It authenticates with the dashboard's email/password
endpoint (no admin-issued API token required) and caches an access token
on disk. Subsequent commands use that token until it expires.

For an AI agent, the model is simple:

- **Organizations** contain **locations**.
- **Locations** contain **spaces** (meeting rooms, desks, etc).
- The user's `quick_book` config picks a default location and a
  prioritized list of room numbers; `robin book` (alias `robin now`)
  uses that to auto-pick the best free room.

## When to use robin

- **"Book me a room now"** → `robin book` (or `robin now`).
- **"Book a room tomorrow at 9"** → `robin book --start 'tomorrow 9am'`.
- **"What rooms exist?"** → `robin spaces`.
- **"Show me the pick without booking"** → `robin book --dry-run`.
- **"Which org/location am I on?"** → `robin whoami`, `robin orgs`,
  `robin locations`.

If a command returns `not authenticated` or a 401, robin will try a
single auto-login using cached credentials before failing — you don't
need to run `robin login` yourself unless that retry fails.

## Read-only commands you may run freely

These don't book anything or change remote state. Run them as needed:

```bash
robin whoami                    # current user (id, email, time zone)
robin whoami --json             # structured output

robin orgs                      # organizations the user belongs to
robin locations                 # locations in the active org
robin locations --org <id>      # locations for a specific org

robin spaces                    # spaces in the configured quick_book location
robin spaces --location <id>    # spaces in a specific location
robin spaces --json             # structured output

robin book --dry-run            # auto-pick best room, print it, don't book
robin book --dry-run --json     # structured pick (space_id, start, end, duration_minutes)
```

## Booking modes

`robin book` has two modes, picked by whether `--space` is supplied:

### Auto-pick (default)

Finds the best available room based on the user's `quick_book` config.
`--start` is the earliest acceptable start time; the picker takes the
longest free slot up to `--max`.

```bash
robin book                                # best room, starting now
robin book --start 'tomorrow 9am'         # best room tomorrow at 9
robin book --start now --duration 1h      # exactly 1h, starting now
robin book --max 60                       # cap at 60 minutes
robin book --prioritize-length            # take the longest slot, ignore priority
```

### Specific space (--space)

Books that exact space. `--start` is the *exact* start time. Provide
`--duration` OR `--end`, not both.

```bash
robin book --space 172344 --start '14:00' --duration 30m
robin book --space 172344 --start 'tomorrow 9am' --duration 1h --yes
```

In specific mode, robin prompts for confirmation unless `--yes` is
passed. With `--no-input` (or no TTY) the command refuses to book
without `--yes` — pair them for non-interactive use.

## Time expressions

`--start`, `--end`, and `--duration` accept several formats. Pick the
narrowest one that expresses the user's intent — natural language is fine
but a strict datetime is more predictable for scripts.

| Form              | Examples                                                        |
|-------------------|-----------------------------------------------------------------|
| literal           | `now`                                                           |
| Go duration       | `30m`, `1h`, `2h30m`                                            |
| clock time today  | `14:00`, `09:30`                                                |
| strict datetime   | `2026-04-29 09:00`, `2026-04-29T09:00`, RFC3339                 |
| natural language  | `tomorrow`, `tomorrow 9am`, `monday 9am`, `in 2 hours`, `noon`  |

Ambiguous phrases like `monday` resolve to the **upcoming** Monday,
not the past one — robin sets the natural-date direction to future.

## Output and JSON

Every command supports `--json` for machine-readable output. Use this
when you need to chain results (e.g. resolve a space ID, then book it).

Auto-pick JSON shape:

```json
{
  "dry_run": true,
  "space_id": 172344,
  "space_name": "Room 4.05 — Aurora",
  "start": "2026-05-06T09:00:00+02:00",
  "end": "2026-05-06T10:00:00+02:00",
  "duration_minutes": 60
}
```

When the booking is real (not `--dry-run`), the response also includes
`event_id`.

## Commands you should NOT run unprompted

These either change account state or write a real calendar event. Confirm
the user's intent before invoking:

- `robin init` — interactive setup that writes config (email, password
  hash, default org/location). Only run when the user is bootstrapping.
- `robin login` — exchanges credentials for a fresh access token. robin
  retries this automatically on 401, so you usually don't need to call it.
- `robin logout` — clears the cached session.
- `robin book` (without `--dry-run`) — creates a real booking. Always
  prefer `--dry-run` first when exploring; only book when the user has
  asked for it.

## Global flags worth knowing

- `--json` — machine-readable output.
- `-q` / `--quiet` — suppress non-essential output.
- `-v` / `--verbose` — show HTTP exchange and per-room availability.
- `--no-color` — disable color (also honors `NO_COLOR`).
- `--no-input` — fail rather than prompt; pair with `--yes` on `book`
  for non-interactive booking.
- `-y` / `--yes` — skip the specific-mode confirmation prompt.

## Examples

### Book the best room right now

```bash
robin book
```

### Preview tomorrow's 9am pick before committing

```bash
robin book --start 'tomorrow 9am' --dry-run --json
```

### Find a specific room ID, then book it

```bash
robin spaces --json | jq '.[] | select(.name | test("Aurora")) | .id'
robin book --space <id> --start '14:00' --duration 30m --yes
```

### Check what location the user is configured against

```bash
robin whoami
robin locations
```

## Failure modes worth recognizing

- **`not authenticated` / 401 / `expired`** — robin tries auto-login
  once. If that still fails, the user's saved credentials are likely
  wrong; suggest `robin init` or `robin login`.
- **`auto-pick requires quick_book in config`** — the user hasn't run
  `robin init` or hasn't filled out `quick_book`. Auto-pick can't
  work without a default location and priority list.
- **`no rooms available in the next <window>`** — every priority room
  (and every fallback meeting room) is booked. Suggest widening the
  window with `--window 60`, or `--prioritize-length` to ignore the
  priority list.
- **`Robin requires events to be at least 5 minutes`** — the requested
  duration was under 5 minutes. Bump it up.
