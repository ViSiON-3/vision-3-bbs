package tosser

import (
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// resolveOrigAddr returns the author's FTN address for an inbound packed
// message.
//
// The packed message header (FTS-0001) carries only net and node, so a point
// author's ".N" has to be recovered from one of the places that does carry it:
// the MSGID kludge, the origin line, an FMPT kludge, or the packet header.
// Without this a point node arrives as a plain 3D address and the reader shows
// (and replies quote) the boss node instead.
func (t *Tosser) resolveOrigAddr(pktHdr *ftn.PacketHeader, msg *ftn.PackedMessage, parsed *ftn.ParsedBody, msgID string) string {
	zone := pktHdr.OrigZone
	if zone == 0 {
		zone = pktHdr.QOrigZone // Fallback to QMail zone field
	}
	if zone == 0 {
		zone = uint16(t.ownAddr.Zone) // Last resort: assume same zone
	}

	addr := jam.FidoAddress{
		Zone: int(zone),
		Net:  int(msg.OrigNet),
		Node: int(msg.OrigNode),
	}
	// On netmail gated between zones the packet header carries the gate's
	// zone, not the author's; INTL carries the author's.
	if a := intlAddr(parsed.Kludges, intlOrig); a != nil && a.Net == addr.Net && a.Node == addr.Node {
		addr.Zone = a.Zone
	}
	addr.Point = origPoint(addr, pktHdr, parsed, msgID)
	return addr.String()
}

// resolveDestAddr returns the addressee's FTN address for an inbound packed
// message, or "" when the message carries no destination (echomail zeroes the
// packed header's destination fields).
//
// A netmail destination is spread across three places: the packed header
// (FTS-0001) carries net and node, while the zone and point arrive as
// addressing control paragraphs in the message body (FTS-4001) — INTL for the
// zone, TOPT for the point. Nothing used to read them, so inbound netmail
// reached the reader with the addressee's name but no address at all.
func (t *Tosser) resolveDestAddr(pktHdr *ftn.PacketHeader, msg *ftn.PackedMessage, parsed *ftn.ParsedBody) string {
	if msg.DestNet == 0 && msg.DestNode == 0 {
		return ""
	}

	zone := pktHdr.DestZone
	if zone == 0 {
		zone = pktHdr.QDestZone // Fallback to QMail zone field
	}
	if zone == 0 {
		zone = uint16(t.ownAddr.Zone) // Last resort: assume same zone
	}

	addr := jam.FidoAddress{
		Zone: int(zone),
		Net:  int(msg.DestNet),
		Node: int(msg.DestNode),
	}
	if a := intlAddr(parsed.Kludges, intlDest); a != nil && a.Net == addr.Net && a.Node == addr.Node {
		addr.Zone = a.Zone
	}
	addr.Point = destPoint(addr, pktHdr, parsed)
	return addr.String()
}

// destPoint digs the addressee's point number out of an inbound message, or
// returns 0 when the addressee is a full node. As with origPoint, the packet
// header is only trusted when the packet was addressed to the same net/node as
// the message itself.
func destPoint(base jam.FidoAddress, pktHdr *ftn.PacketHeader, parsed *ftn.ParsedBody) int {
	// The TOPT control paragraph (FTS-4001) is the destination point, written
	// into the body of netmail addressed to a point.
	if p, ok := kludgePoint(parsed.Kludges, "TOPT "); ok {
		return p
	}
	if pktHdr.DestPoint != 0 && int(pktHdr.DestNet) == base.Net && int(pktHdr.DestNode) == base.Node {
		return int(pktHdr.DestPoint)
	}
	return 0
}

// Field positions in an "INTL <destination> <origin>" control paragraph
// (FTS-4001).
const (
	intlDest = 1
	intlOrig = 2
)

// intlAddr returns the INTL address at the given field position, or nil when
// there is no usable INTL line. INTL is netmail-only and never carries a
// point, so it is read for its zone alone.
func intlAddr(kludges []string, field int) *jam.FidoAddress {
	for _, k := range kludges {
		if !strings.HasPrefix(k, "INTL ") {
			continue
		}
		f := strings.Fields(k)
		if len(f) <= field {
			continue
		}
		if a, err := jam.ParseAddress(f[field]); err == nil && a.Zone > 0 {
			return a
		}
	}
	return nil
}

// kludgePoint returns the point number carried by the first kludge with the
// given prefix ("FMPT " or "TOPT ").
func kludgePoint(kludges []string, prefix string) (int, bool) {
	for _, k := range kludges {
		v, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		if p, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && p > 0 {
			return p, true
		}
	}
	return 0, false
}

// origPoint digs the author's point number out of an inbound message, or
// returns 0 when the author is a full node.
//
// Every candidate is checked against the packed header's net/node before it is
// trusted: echomail reaches us relayed, so the packet was written by a link
// rather than the author, and a point link's own packet header must not stamp
// its point number onto everyone else's mail.
func origPoint(base jam.FidoAddress, pktHdr *ftn.PacketHeader, parsed *ftn.ParsedBody, msgID string) int {
	matches := func(a *jam.FidoAddress) bool {
		return a.Point != 0 && a.Net == base.Net && a.Node == base.Node
	}

	// MSGID (FTS-0009) is "<address> <serial>" and names the author, making it
	// the most reliable source. The address half can be an @-style ID rather
	// than an FTN address, which simply fails to parse and falls through.
	if fields := strings.Fields(msgID); len(fields) > 0 {
		if a, err := jam.ParseAddress(fields[0]); err == nil && matches(a) {
			return a.Point
		}
	}

	// The origin line (FTS-0004) carries the author's 4D address.
	if origin := jam.ExtractOriginAddress(parsed.Text); origin != "" {
		if a, err := jam.ParseAddress(origin); err == nil && matches(a) {
			return a.Point
		}
	}

	// The FMPT control paragraph (FTS-4001) is the sender's point, written
	// into the body of netmail sent from a point.
	if p, ok := kludgePoint(parsed.Kludges, "FMPT "); ok {
		return p
	}

	// The Type-2+ packet header, but only when the packet was written by the
	// author itself — direct netmail from a point, typically.
	if pktHdr.OrigPoint != 0 && int(pktHdr.OrigNet) == base.Net && int(pktHdr.OrigNode) == base.Node {
		return int(pktHdr.OrigPoint)
	}

	return 0
}
