// Package qwkservice provides packet export/import orchestration on top of the
// low-level QWK codec in internal/qwk. It owns the "business logic" of turning a
// user's message bases into a QWK packet and of importing a REP reply packet
// back into the message store, so that callers (terminal menus today, a packet
// transport API later) only deal with transport and UI concerns.
package qwkservice

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// defaultMaxPerArea caps how many messages are packed from a single area to
// keep packet sizes reasonable.
const defaultMaxPerArea = 500

// MessageStore is the subset of *message.MessageManager that the QWK service
// depends on. Defining it here keeps the service unit-testable with a fake and
// avoids coupling the service to the full manager surface.
type MessageStore interface {
	ListAreas() []*message.MessageArea
	GetAreaByTag(tag string) (*message.MessageArea, bool)
	GetAreaByID(id int) (*message.MessageArea, bool)
	GetLastRead(areaID int, username string) (int, error)
	SetLastRead(areaID int, username string, msgNum int) error
	GetMessageCountForArea(areaID int) (int, error)
	GetMessage(areaID, msgNum int) (*message.DisplayMessage, error)
	AddMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error)
	AddPrivateMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error)
}

// Service orchestrates QWK packet export and REP import for a single BBS
// identity.
type Service struct {
	store       MessageStore
	bbsID       string
	bbsName     string
	sysOpName   string
	confMapPath string
}

// New creates a QWK service. bbsID is the short packet identifier (e.g.
// "VISION3"); bbsName and sysOpName populate CONTROL.DAT; dataPath is the base
// data directory used to persist the stable conference map.
func New(store MessageStore, bbsID, bbsName, sysOpName, dataPath string) *Service {
	return &Service{
		store:       store,
		bbsID:       bbsID,
		bbsName:     bbsName,
		sysOpName:   sysOpName,
		confMapPath: filepath.Join(dataPath, "qwk_conferences.json"),
	}
}

// loadConfMap loads the conference map, syncs it against the current areas, and
// persists it if anything changed.
func (s *Service) loadConfMap() (*ConferenceMap, error) {
	cm, err := LoadConferenceMap(s.confMapPath)
	if err != nil {
		return nil, err
	}
	if cm.Sync(s.store.ListAreas()) {
		if err := cm.Save(s.confMapPath); err != nil {
			return nil, err
		}
	}
	return cm, nil
}

// LastReadUpdate records a pending newscan pointer advance for one area.
type LastReadUpdate struct {
	AreaID int
	MsgNum int
}

// ExportOptions configure a packet build.
type ExportOptions struct {
	Handle string // user handle (used for PERSONAL.NDX and last-read)
	// TaggedTags lists the area tags to export. When empty, the service falls
	// back to every loaded area (ListAreas); note this is not access-filtered —
	// callers that need ACS enforcement must pre-filter the tags they pass.
	TaggedTags []string
	MaxPerArea int // per-area message cap; <= 0 uses the default
}

// ExportResult is the outcome of BuildPacket.
type ExportResult struct {
	BBSID        string
	Packet       []byte // complete .QWK zip; nil when MessageCount == 0
	MessageCount int
	// LastRead holds the newscan advances that should be committed only after a
	// successful transfer. Apply them with CommitExport.
	LastRead []LastReadUpdate
}

// BuildPacket gathers new messages from the user's areas and produces a QWK
// packet. It does not advance last-read pointers; the caller must call
// CommitExport after the packet is successfully delivered.
func (s *Service) BuildPacket(opts ExportOptions) (*ExportResult, error) {
	maxPerArea := opts.MaxPerArea
	if maxPerArea <= 0 {
		maxPerArea = defaultMaxPerArea
	}

	cm, err := s.loadConfMap()
	if err != nil {
		return nil, err
	}

	pw := qwk.NewPacketWriter(s.bbsID, s.bbsName, s.sysOpName)
	pw.SetPersonalTo(opts.Handle)

	// Resolve the area tags to export. Build a fresh slice (never alias or
	// append into the caller's TaggedTags backing array) and drop duplicates so
	// a repeated tag cannot produce duplicate conferences or last-read updates.
	var tags []string
	if len(opts.TaggedTags) == 0 {
		for _, area := range s.store.ListAreas() {
			tags = append(tags, area.Tag)
		}
	} else {
		tags = append(tags, opts.TaggedTags...)
	}

	res := &ExportResult{BBSID: s.bbsID}
	seen := make(map[string]struct{}, len(tags))

	for _, tag := range tags {
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}

		area, exists := s.store.GetAreaByTag(tag)
		if !exists {
			continue
		}

		entry, ok := cm.EntryForTag(area.Tag)
		if !ok {
			// Sync guarantees an entry for every area; skip defensively.
			continue
		}
		pw.AddConference(entry.QWKNumber, area.Name)
		isPrivateConf := entry.Kind == KindPrivateMail

		lastRead, err := s.store.GetLastRead(area.ID, opts.Handle)
		if err != nil {
			slog.Warn("qwk export: failed to get lastread", "area", area.ID, "error", err)
			continue
		}

		msgCount, err := s.store.GetMessageCountForArea(area.ID)
		if err != nil {
			slog.Warn("qwk export: failed to get message count", "area", area.ID, "error", err)
			continue
		}

		packed := 0
		highestPacked := lastRead
		for msgNum := lastRead + 1; msgNum <= msgCount && packed < maxPerArea; msgNum++ {
			msg, err := s.store.GetMessage(area.ID, msgNum)
			if err != nil {
				continue
			}
			if msg.IsDeleted {
				continue
			}
			if isPrivateConf && !ownsPrivateMessage(msg, opts.Handle) {
				continue
			}

			pw.AddMessage(qwk.PacketMessage{
				Conference: entry.QWKNumber,
				Number:     msg.MsgNum,
				From:       msg.From,
				To:         msg.To,
				Subject:    msg.Subject,
				DateTime:   msg.DateTime,
				Body:       msg.Body,
				Private:    msg.IsPrivate,
			})
			packed++
			res.MessageCount++
			if msgNum > highestPacked {
				highestPacked = msgNum
			}
		}

		if packed > 0 {
			newLastRead := highestPacked
			if newLastRead > msgCount {
				newLastRead = msgCount
			}
			res.LastRead = append(res.LastRead, LastReadUpdate{AreaID: area.ID, MsgNum: newLastRead})
		}
	}

	if res.MessageCount == 0 {
		return res, nil
	}

	var buf bytes.Buffer
	if err := pw.WritePacket(&buf); err != nil {
		return nil, err
	}
	res.Packet = buf.Bytes()
	return res, nil
}

// ownsPrivateMessage reports whether a private message belongs to the given
// user (addressed to or sent by them).
func ownsPrivateMessage(msg *message.DisplayMessage, handle string) bool {
	return msg.IsPrivate && (strings.EqualFold(msg.To, handle) || strings.EqualFold(msg.From, handle))
}

// CommitExport applies the deferred newscan pointer advances from a successful
// export. Failures are logged and skipped; partial commits are tolerated.
func (s *Service) CommitExport(handle string, res *ExportResult) {
	if res == nil {
		return
	}
	for _, upd := range res.LastRead {
		if err := s.store.SetLastRead(upd.AreaID, handle, upd.MsgNum); err != nil {
			slog.Warn("qwk export: failed to update lastread", "area", upd.AreaID, "error", err)
		}
	}
}

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

	res := &ImportResult{}
	for _, msg := range msgs {
		area, exists := s.store.GetAreaByID(msg.Conference)
		if !exists {
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

		if _, err := s.store.AddMessage(area.ID, opts.Handle, msg.To, msg.Subject, body, ""); err != nil {
			slog.Error("qwk import: failed to post", "area", area.ID, "error", err)
			res.Skipped++
			continue
		}
		res.Posted++
	}

	return res, nil
}
