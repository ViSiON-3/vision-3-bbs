package jam

import "strings"

// MessageType represents the type of message being created.
type MessageType int

const (
	MsgTypeLocalMsg    MessageType = iota // Local BBS-only message
	MsgTypeEchomailMsg                    // FTN conference/echo message
	MsgTypeNetmailMsg                     // FTN direct network mail
)

// IsEchomail reports whether this is an echomail message.
func (mt MessageType) IsEchomail() bool { return mt == MsgTypeEchomailMsg }

// IsNetmail reports whether this is a netmail message.
func (mt MessageType) IsNetmail() bool { return mt == MsgTypeNetmailMsg }

// IsLocal reports whether this is a local message.
func (mt MessageType) IsLocal() bool { return mt == MsgTypeLocalMsg }

// GetJAMAttribute returns the JAM attribute flags for this message type.
//
// Netmail carries MsgPrivate: it is mail addressed to one person, private by
// definition, and the tosser stamps MSGPRIVATE on every netmail it packs. Every
// netmail reaches a base through WriteMessageExt, so setting the flag here
// covers inbound tossed mail and locally written mail alike, without each
// caller having to remember.
func (mt MessageType) GetJAMAttribute() uint32 {
	switch mt {
	case MsgTypeEchomailMsg:
		return MsgLocal | MsgTypeEcho
	case MsgTypeNetmailMsg:
		return MsgLocal | MsgTypeNet | MsgPrivate
	default:
		return MsgLocal | MsgTypeLocal
	}
}

// DetermineMessageType returns the MessageType based on area configuration.
//
// A qwknet area is echomail as far as the base is concerned: its posts are
// conference mail that leaves the system, so they need DateProcessed left at
// zero for the QWK network scanner to find them, and a MSGID for threading on
// the far side. The FTN tosser never touches them because it selects areas by
// AreaType and Network, not by JAM attribute.
func DetermineMessageType(areaType, echoTag string) MessageType {
	switch strings.ToLower(strings.TrimSpace(areaType)) {
	case "echo", "echomail", "qwknet":
		return MsgTypeEchomailMsg
	case "netmail", "direct":
		return MsgTypeNetmailMsg
	default:
		return MsgTypeLocalMsg
	}
}
