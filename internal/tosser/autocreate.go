package tosser

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// autoCreateArea creates a message area for an echo this network has no area
// for, when the network's auto_create is enabled (see
// config.FTNAutoCreateConfig). It returns false when auto-create is off or
// the area could not be created, and the caller falls back to the bad area.
//
// Only packets from one of the network's links get this far (tossPacket
// skips the rest), so a stranger cannot create areas by sending mail.
func (t *Tosser) autoCreateArea(echoTag string) (*message.MessageArea, bool) {
	ac := t.config.AutoCreate
	if !ac.Enabled {
		return nil, false
	}
	if !ftn.ValidEchoTag(echoTag) {
		slog.Warn("auto-create: not a valid echo tag, not creating an area",
			"network", t.networkName, "area", echoTag)
		return nil, false
	}

	// Named the way the setup wizard names the areas it creates, so the
	// tag can't collide with another network carrying the same echo, and
	// each network keeps its own message base.
	netKey := strings.ToLower(t.networkName)
	lowerEcho := strings.ToLower(echoTag)
	area := message.MessageArea{
		Tag:          netKey + "_" + lowerEcho,
		Name:         echoTag,
		AreaType:     "echomail",
		Network:      t.networkName,
		EchoTag:      echoTag,
		OriginAddr:   t.config.OwnAddress,
		ACSRead:      ac.ReadACS(),
		ACSWrite:     ac.WriteACS(),
		ConferenceID: t.autoCreateConferenceID(),
		MaxMessages:  ac.MaxMessagesOrDefault(),
		MaxAge:       ac.MaxAgeOrDefault(),
		AutoJoin:     ac.AutoJoinOrDefault(),
		RealNameOnly: ac.RealNameOnly,
		BasePath:     filepath.Join("msgbases", "fn."+netKey+"_"+lowerEcho),
	}

	id, err := t.msgMgr.AddArea(area)
	if err != nil {
		slog.Warn("auto-create: could not create message area",
			"network", t.networkName, "area", echoTag, "tag", area.Tag, "error", err)
		return nil, false
	}
	created, ok := t.msgMgr.GetAreaByID(id)
	if !ok {
		slog.Warn("auto-create: created message area is missing",
			"network", t.networkName, "area", echoTag, "id", id)
		return nil, false
	}
	slog.Info("auto-created echomail area",
		"network", t.networkName, "area", echoTag, "tag", created.Tag, "id", id,
		"conference_id", created.ConferenceID)
	return created, true
}

// autoCreateConferenceID returns the conference for an auto-created area: the
// configured one, or else the conference of the network's other echomail
// areas (v3mail has no conference list of its own to look one up in).
func (t *Tosser) autoCreateConferenceID() int {
	if id := t.config.AutoCreate.ConferenceID; id != 0 {
		return id
	}
	for _, a := range t.msgMgr.ListAreas() {
		if strings.EqualFold(a.Network, t.networkName) && strings.EqualFold(a.AreaType, "echomail") && a.ConferenceID != 0 {
			return a.ConferenceID
		}
	}
	return 0
}
