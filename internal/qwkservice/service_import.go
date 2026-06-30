package qwkservice

import (
	"bytes"
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// ImportOptions configure a REP import.
type ImportOptions struct {
	// Handle is the posting user's handle (becomes the message From).
	Handle string
	// Signature, when non-empty, is appended to each imported message body.
	Signature string
	// Authorize, when set, gates posting per area (e.g. an ACS write check).
	// Returning false skips the message. A nil Authorize allows all areas.
	Authorize func(area *message.MessageArea) bool
	// Notify, when set, is called just before posting to an area. It is a UI
	// hook (e.g. printing "Posting to <area>") and must not block.
	Notify func(area *message.MessageArea)
}

// ImportResult summarizes a REP import.
type ImportResult struct {
	Posted  int
	Skipped int
}

// ImportREP parses a REP packet and posts its replies into the message store,
// routing each message to its conference's area. Unknown areas, unauthorized
// areas, and post failures are skipped (and counted), so a single bad message
// does not abort the whole import.
func (s *Service) ImportREP(data []byte, opts ImportOptions) (*ImportResult, error) {
	msgs, err := qwk.ReadREP(bytes.NewReader(data), int64(len(data)), s.bbsID)
	if err != nil {
		return nil, err
	}

	cm, err := s.loadConfMap()
	if err != nil {
		return nil, err
	}

	res := &ImportResult{}
	for _, msg := range msgs {
		area, kind, ok := s.resolveConference(cm, msg.Conference)
		if !ok {
			slog.Warn("qwk import: unknown conference, skipping", "conference", msg.Conference)
			res.Skipped++
			continue
		}

		if opts.Authorize != nil && !opts.Authorize(area) {
			slog.Warn("qwk import: not authorized to post, skipping", "tag", area.Tag)
			res.Skipped++
			continue
		}

		if opts.Notify != nil {
			opts.Notify(area)
		}

		body := msg.Body
		if opts.Signature != "" {
			body = body + "\n\n" + opts.Signature
		}

		var perr error
		if kind == KindPrivateMail {
			_, perr = s.store.AddPrivateMessage(area.ID, opts.Handle, msg.To, msg.Subject, body, "")
		} else {
			_, perr = s.store.AddMessage(area.ID, opts.Handle, msg.To, msg.Subject, body, "")
		}
		if perr != nil {
			slog.Error("qwk import: failed to post", "area", area.ID, "error", perr)
			res.Skipped++
			continue
		}
		res.Posted++
	}

	return res, nil
}

// resolveConference maps a QWK conference number to a local area and its kind.
// It prefers the stable conference map; if the number is unmapped (e.g. a packet
// produced before the map existed, whose public numbers equal area IDs), it
// falls back to a direct area-ID lookup and treats the result as public.
func (s *Service) resolveConference(cm *ConferenceMap, number int) (*message.MessageArea, ConferenceKind, bool) {
	if entry, ok := cm.EntryForNumber(number); ok {
		// The number is mapped: the entry's area is its only valid destination.
		// If that area no longer exists the map is stale; do NOT fall back to a
		// numeric-ID lookup, which could resolve to an unrelated area and post
		// the reply into the wrong place.
		if area, exists := s.store.GetAreaByTag(entry.AreaTag); exists {
			return area, entry.Kind, true
		}
		return nil, KindPublic, false
	}
	// Unmapped number: a legacy packet predating the map, whose public numbers
	// equalled local area IDs. Fall back to a direct area-ID lookup as public.
	if area, exists := s.store.GetAreaByID(number); exists {
		return area, KindPublic, true
	}
	return nil, KindPublic, false
}
