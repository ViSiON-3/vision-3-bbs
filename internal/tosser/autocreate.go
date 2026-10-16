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
// config.FTNAutoCreateConfig). noCreate is why the packet the message came in
// may not create areas, or "" if it may (see autoCreateRefusal). It returns
// false when the area was not created, and the caller falls back to the bad
// area.
func (t *Tosser) autoCreateArea(echoTag, noCreate string) (*message.MessageArea, bool) {
	ac := t.config.AutoCreate
	if !ac.Enabled {
		return nil, false
	}
	if noCreate != "" {
		slog.Warn("auto-create: not creating an area for mail from this packet",
			"network", t.networkName, "area", echoTag, "reason", noCreate)
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
		if strings.EqualFold(a.Network, t.networkName) && isEchoAreaType(a.AreaType) && a.ConferenceID != 0 {
			return a.ConferenceID
		}
	}
	return 0
}

// autoCreateRefusal says why mail in a packet with header hdr may not create
// areas, or returns "" if it may. Creating an area changes the BBS's config,
// so, as for TICs, the packet must come from one of the network's links and
// prove it: either over a password-checked session (the secure inbound) or by
// carrying the link's packet password. The origin address alone proves
// nothing, since anyone can write any address into a packet header.
//
// When the link has a packet password, the packet must carry it even in the
// secure inbound.
func (t *Tosser) autoCreateRefusal(hdr *ftn.PacketHeader, secure bool) string {
	zone := hdr.OrigZone
	if zone == 0 {
		zone = hdr.QOrigZone
	}
	// isPacketFromKnownLink lets a packet through when no link address
	// parses; that is not enough to create areas.
	link, ok := t.linkFor(ftn.Address{Zone: int(zone), Net: int(hdr.OrigNet), Node: int(hdr.OrigNode)})
	if !ok {
		return "packet is not from one of the network's links"
	}
	if link.PacketPassword != "" {
		if !packetPasswordMatches(hdr.Password, link.PacketPassword) {
			return "packet password does not match the link's packet_password"
		}
		return ""
	}
	if !secure {
		return "packet arrived in the unsecured inbound and the link has no packet_password to prove it"
	}
	return ""
}

// packetPasswordMatches compares a packet header's password field with a
// link's packet password, ignoring case as FTN tossers do. The field holds
// at most 8 bytes, NUL-padded, so a longer configured password is compared
// on its first 8.
func packetPasswordMatches(field [8]byte, want string) bool {
	got := strings.TrimRight(string(field[:]), "\x00")
	if len(want) > len(field) {
		want = want[:len(field)]
	}
	return strings.EqualFold(got, want)
}

// isEchoAreaType reports whether areaType is an echomail area: "echomail",
// or the older "echo" that the scanner and jam.DetermineMessageType accept.
func isEchoAreaType(areaType string) bool {
	return areaType == "echomail" || areaType == "echo"
}
