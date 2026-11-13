package message

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// UnmarshalJSON reads an area. A V3Net area's name may have been copied from a
// hub by a build that stored it as sent, and screens expand area names, so its
// screen codes and control characters are made harmless wherever it is loaded.
func (a *MessageArea) UnmarshalJSON(data []byte) error {
	type plain MessageArea
	if err := json.Unmarshal(data, (*plain)(a)); err != nil {
		return err
	}
	if a.AreaType == "v3net" {
		a.Name = ansi.DefangScreenCodes(a.Name)
	}
	return nil
}

// MessageArea defines the structure for a message base/forum.
type MessageArea struct {
	ID           int    `json:"id"`                        // Unique local ID for the area
	Position     int    `json:"position"`                  // Display/sort order (1-based)
	Tag          string `json:"tag"`                       // Short, unique tag (e.g., "GENERAL", "FSX_GEN")
	Name         string `json:"name"`                      // Display name
	Description  string `json:"description"`               // Longer description
	ACSRead      string `json:"acs_read"`                  // ACS string required to read
	ACSWrite     string `json:"acs_write"`                 // ACS string required to post
	AllowAnon    *bool  `json:"allow_anonymous,omitempty"` // Optional: allow anonymous posts (nil = no; area must opt in)
	RealNameOnly bool   `json:"real_name_only,omitempty"`  // Require real name for posts in this area
	Autocorrect  *bool  `json:"autocorrect,omitempty"`     // Optional: editor autocorrect here (nil = board default; see AutocorrectOn)
	ConferenceID int    `json:"conference_id,omitempty"`   // Conference this area belongs to (0=ungrouped)
	BasePath     string `json:"base_path"`                 // Relative path to JAM base (e.g., "msgbases/general")
	MaxMessages  int    `json:"max_messages,omitempty"`    // Max messages to retain (0=unlimited)
	MaxAge       int    `json:"max_age,omitempty"`         // Auto-purge messages older than N days (0=unlimited)
	AutoJoin     bool   `json:"auto_join,omitempty"`       // Auto-join this area for new users
	AreaType     string `json:"area_type"`                 // "local", "echomail", "netmail", "v3net", "qwknet"
	EchoTag      string `json:"echo_tag,omitempty"`        // FTN echo tag (e.g., "FSX_GEN"); for qwknet, the hub's conference name
	OriginAddr   string `json:"origin_addr,omitempty"`     // FTN origin address (e.g., "21:3/110")
	Network      string `json:"network,omitempty"`         // Network key (e.g., "fsxnet", "dovenet")
	Sponsor      string `json:"sponsor,omitempty"`         // Handle of the area sponsor/moderator
	// SponsorID is the sponsor's account ID, which decides who the sponsor
	// is; Sponsor is the name shown and typed (#1020). 0 means not yet
	// bound: the BBS binds it to the account holding Sponsor when it loads
	// the areas. NoSponsorID means Sponsor named no account then. Anything
	// that sets Sponsor sets this, or clears it to 0 to have it bound.
	SponsorID int `json:"sponsor_id,omitempty"`
	// QWKConference is the hub's conference number for a qwknet area. It is
	// the only thing a QWK network routes on: the hub numbers its
	// conferences, and every packet header carries that number. 0 = unset.
	QWKConference int `json:"qwk_conference,omitempty"`
}

// AreaTypeQWKNet marks an area fed by a QWK network hub.
const AreaTypeQWKNet = "qwknet"

// IsQWKNet reports whether the area belongs to a QWK network.
func (a *MessageArea) IsQWKNet() bool {
	return a != nil && strings.EqualFold(a.AreaType, AreaTypeQWKNet)
}

// PostingName returns the name a post in the area is signed with: the
// poster's real name when the area requires one and the poster has one,
// otherwise the handle. Private mail outside netmail is signed by handle
// whatever this says; callers posting it should not ask.
func (a *MessageArea) PostingName(handle, realName string) string {
	if a != nil && a.RealNameOnly && strings.TrimSpace(realName) != "" {
		return realName
	}
	return handle
}

// AutocorrectOn reports whether the message editor's autocorrect is on for
// posts in the area: the area's own setting, or boardDefault (config.json
// editorAutocorrect) when the area has none. A nil area (private mail, the
// auto-signature) follows the board. A caller can still turn it off for
// themselves.
func (a *MessageArea) AutocorrectOn(boardDefault bool) bool {
	if a == nil || a.Autocorrect == nil {
		return boardDefault
	}
	return *a.Autocorrect
}

// DisplayMessage is a high-level message view for the UI layer.
// It wraps the JAM binary data into a form suitable for display and
// interaction in the message reader/composer.
type DisplayMessage struct {
	MsgNum     int // 1-based message number in the JAM base
	From       string
	To         string
	Subject    string
	DateTime   time.Time
	Body       string // Decoded message body for display
	MsgID      string // FTN MSGID (for reply linking)
	ReplyID    string // FTN REPLYID (message this replies to)
	ReplyToNum int    // JAM ReplyTo: message number of parent (0 = none)
	OrigAddr   string // FTN origin address
	DestAddr   string // FTN destination address
	Attributes uint32 // JAM message attribute flags
	IsPrivate  bool
	IsDeleted  bool
	AreaID     int // Area this message belongs to
	// Charset is the character set the message's CHRS kludge names, upper
	// case and without its level ("CP437", "UTF-8"), or "" if it has none.
	Charset string
	// SenderHandle is the handle of the local account that wrote the
	// message when From is another name (PostOptions.SenderHandle).
	SenderHandle string
}

// Constants for standard message fields.
const (
	MsgToUserAll = "All" // Standard To value for public messages
)

// NoSponsorID is MessageArea.SponsorID when Sponsor named no account as the
// areas were loaded. Nobody is that area's sponsor until it is set again.
const NoSponsorID = -1

// KeepsSponsorBinding reports whether setting a's sponsor to name leaves its
// account binding as it is: the same name, already bound to an account. A
// sponsor who was renamed keeps the area that way, and so does pressing
// Enter on the field, even after someone else has taken the old handle. A
// binding to nobody (NoSponsorID) is not kept, so entering the name again
// binds it once the account exists. To hand an area to whoever holds the
// name now, clear the sponsor and set it again.
func (a MessageArea) KeepsSponsorBinding(name string) bool {
	return a.SponsorID > 0 && strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(a.Sponsor))
}

// SponsoredBy reports whether the account with ID id is a's sponsor.
func (a MessageArea) SponsoredBy(id int) bool {
	return id > 0 && a.Sponsor != "" && a.SponsorID == id
}
