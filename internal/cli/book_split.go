package cli

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/simenandre/robin-cli/internal/robin"
)

type bookChunk struct {
	Start time.Time
	End   time.Time
}

// bookAdaptive POSTs the booking as a single event first. If Robin rejects
// it with a too-long policy violation, we read the cap out of the error,
// confirm with the user, then book the run as back-to-back chunks of the
// reported cap in the same space. Returns every event ID created.
//
// skipPrompt bypasses the split confirm — used both for non-interactive
// callers and for the recursive case where the user already OK'd the split.
func bookAdaptive(io *IO, c *robin.Client, spaceID int64, baseReq robin.BookRequest, start, end time.Time, tzName, roomName string, skipPrompt bool) ([]string, error) {
	ev, err := bookOne(c, spaceID, baseReq, start, end, tzName)
	if err == nil {
		return []string{ev}, nil
	}
	apiErr, isAPI := robin.AsAPIError(err)
	if !isAPI {
		return nil, err
	}
	cap, isTooLong := apiErr.TooLongBooking()
	if !isTooLong {
		return nil, err
	}

	chunks := splitIntoChunks(start, end, cap)
	if len(chunks) < 2 {
		// Robin said too long but our split produced one chunk — bail out
		// rather than loop forever.
		return nil, fmt.Errorf("Robin reports cap %v but split produced no chunks: %w", cap, err)
	}
	io.Status("Robin caps a single booking at %v in %s — splitting into %d back-to-back bookings",
		cap, roomName, len(chunks))

	ok, err := confirmSplit(io, chunks, roomName, skipPrompt)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("aborted")
	}

	var ids []string
	for i, ch := range chunks {
		// Recurse so a chunk that still hits a cap (defensive) gets split
		// further; the user already confirmed once, so skip the prompt.
		chunkIDs, err := bookAdaptive(io, c, spaceID, baseReq, ch.Start, ch.End, tzName, roomName, true)
		ids = append(ids, chunkIDs...)
		if err != nil {
			return ids, fmt.Errorf("chunk %d/%d at %s: %w", i+1, len(chunks), ch.Start.Format("15:04"), err)
		}
	}
	return ids, nil
}

func bookOne(c *robin.Client, spaceID int64, baseReq robin.BookRequest, start, end time.Time, tzName string) (string, error) {
	req := baseReq
	req.Start = robin.DateTime{DateTime: start.Format(time.RFC3339), TimeZone: tzName}
	req.End = robin.DateTime{DateTime: end.Format(time.RFC3339), TimeZone: tzName}
	ev, err := c.BookSpace(spaceID, req)
	if err != nil {
		return "", err
	}
	return ev.ID, nil
}

// splitIntoChunks divides [start, end) into back-to-back runs of at most
// max. Returns nil when the run already fits in one chunk; callers should
// treat that as "no split needed".
func splitIntoChunks(start, end time.Time, max time.Duration) []bookChunk {
	if max <= 0 || end.Sub(start) <= max {
		return nil
	}
	var out []bookChunk
	cur := start
	for cur.Before(end) {
		ce := cur.Add(max)
		if ce.After(end) {
			ce = end
		}
		out = append(out, bookChunk{Start: cur, End: ce})
		cur = ce
	}
	return out
}

// confirmSplit asks the user to OK a multi-chunk booking. Returns true when
// the user confirms or when the call is non-interactive (no TTY, --no-input,
// or skipPrompt set).
func confirmSplit(io *IO, chunks []bookChunk, roomName string, skipPrompt bool) (bool, error) {
	if skipPrompt || io.NoInput || !io.StdinTTY() {
		return true, nil
	}
	desc := fmt.Sprintf("%d back-to-back bookings in %s:\n\n%s",
		len(chunks), roomName, formatChunkList(chunks))
	ok := true
	if err := huh.NewConfirm().
		Title("Split into multiple bookings?").
		Description(desc).
		Affirmative("Yes, book them all").
		Negative("Cancel").
		Value(&ok).
		Run(); err != nil {
		return false, fmt.Errorf("book: %w", err)
	}
	return ok, nil
}

// promptRoomPick lets the user choose a room from the candidates the slot
// picker already deemed available. Candidates should be pre-sorted by the
// caller (priority first, then length) so the top of the list is the most
// likely pick.
func promptRoomPick(candidates []slot, tz *time.Location, bufBefore, bufAfter time.Duration) (slot, error) {
	options := make([]huh.Option[int], 0, len(candidates))
	for i, c := range candidates {
		meetingStart := c.start.In(tz).Add(bufBefore)
		meetingDur := c.duration - bufBefore - bufAfter
		label := c.space.Name
		if c.space.Capacity > 0 {
			label += fmt.Sprintf("  (cap %d)", c.space.Capacity)
		}
		label += fmt.Sprintf(" — %s for %v",
			meetingStart.Format("15:04"), meetingDur)
		if c.priorityRank < math.MaxInt {
			label += fmt.Sprintf("  [priority %d]", c.priorityRank+1)
		}
		options = append(options, huh.NewOption(label, i))
	}
	chosen := 0
	if err := huh.NewSelect[int]().
		Title("Pick a room").
		Description("Type / to filter.").
		Filtering(true).
		Options(options...).
		Value(&chosen).
		Run(); err != nil {
		return slot{}, fmt.Errorf("book: %w", err)
	}
	return candidates[chosen], nil
}

func formatChunkList(chunks []bookChunk) string {
	lines := make([]string, len(chunks))
	for i, c := range chunks {
		lines[i] = fmt.Sprintf("  %d. %s–%s  (%v)",
			i+1,
			c.Start.Format("Mon Jan 2 15:04"),
			c.End.Format("15:04"),
			c.End.Sub(c.Start))
	}
	return strings.Join(lines, "\n")
}
