// Package user owns the BBS's user accounts. UserMgr (NewUserManager) loads
// users.json into memory and is the single point for looking up, creating,
// updating and saving User records; it also authenticates callers by
// password or SSH public key, keeps call history and the online-user set,
// and soft-deletes and purges accounts. Saves merge in changes the sysop made
// concurrently with the ./ue user editor instead of overwriting them.
package user

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// MaxLastLogins = 10 // Max number of last logins to store (Moved to manager.go)

// LoginEvent holds information about a single login
type LoginEvent struct {
	Handle    string
	Timestamp time.Time
}

// TwitEntry identifies a local account or a network author to hide.
type TwitEntry struct {
	UserID int    `json:"userId,omitempty"`
	Name   string `json:"name"`
	Addr   string `json:"addr,omitempty"`
}

// User represents a user account.
type User struct {
	TwitList []TwitEntry `json:"twitList,omitempty"`
	// gen tracks how many times this record has been refreshed from an
	// external edit. Sessions carry a copy taken at some point in the past; if
	// the authoritative record has since moved ahead, that copy must not be
	// allowed to write sysop-owned fields back. Unexported, so it is never
	// serialized to users.json.
	gen uint64
	// persisted records whether this account has ever reached users.json.
	// It distinguishes a record the editor deleted from one this process
	// created and has not written out yet. Unexported, so it is never
	// serialized.
	persisted bool

	ID             int       `json:"id"`           // Added User ID for ACS 'U' check
	PasswordHash   string    `json:"passwordHash"` // Changed from []byte to string
	Handle         string    `json:"handle"`
	LegacyUsername string    `json:"username,omitempty"` // Migration only: used during load when Handle is absent; cleared on save
	AccessLevel    int       `json:"accessLevel"`
	Flags          string    `json:"flags"`                 // Added Flags string for ACS 'F' check (e.g., "XYZ")
	PublicKeys     []string  `json:"publicKeys,omitempty"`  // OpenSSH authorized_keys lines authorized for WFC admin
	WFCReadOnly    bool      `json:"wfcReadOnly,omitempty"` // WFC console may watch only: no kick, snoop, type-in or chat
	LastLogin      time.Time `json:"lastLogin"`

	// Roles are the admin roles the sysop has given this user, by name, as
	// configs/roles.json defines them (internal/perm). Only a sysop sets
	// them; a role the file doesn't define grants nothing.
	Roles []string `json:"roles,omitempty"`
	// Groups are the user groups the sysop has put this user in, by name,
	// as configs/roles.json defines them, for the ACS term GROUP:. Being in
	// a group grants no admin permission.
	Groups []string `json:"groups,omitempty"`
	// PreviousHandles are the handles this account had before, oldest
	// first, recorded on each rename (#1048). Lastread is kept per handle,
	// so what the account read under an earlier handle is found by them.
	PreviousHandles []string `json:"previousHandles,omitempty"`

	// PreviousLogin is the LastLogin value from before the current session's
	// authentication. Authenticate() overwrites LastLogin with time.Now()
	// before the login sequence runs, so anything asking "what is new since
	// this user was last here?" (news, rumors, file newscan) must compare
	// against PreviousLogin, not LastLogin.
	PreviousLogin time.Time `json:"previousLogin,omitempty"`
	// FileNewscanSince overrides the file newscan cutoff. When nil, the file
	// newscan scans "since your previous logon" (PreviousLogin); when set, it
	// scans files uploaded on/after this date. The SETFILESCANDATE command sets
	// it (a date or "all"), and "reset" clears it back to the default.
	// BeginSession clears it, so it lasts only for the session it was set in.
	FileNewscanSince *time.Time `json:"fileNewscanSince,omitempty"`
	TimesCalled      int        `json:"timesCalled"` // Used for E (NumLogons)
	LastBulletinRead time.Time  `json:"lastBulletinRead"`
	RealName         string     `json:"realName"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"` // For optimistic locking - tracks last modification
	Validated        bool       `json:"validated"`
	FilePoints       int        `json:"filePoints"` // Added for P
	NumUploads       int        `json:"numUploads"` // Added for E
	UploadedBytes    int64      `json:"uploadedBytes,omitempty"`
	DownloadedBytes  int64      `json:"downloadedBytes,omitempty"`
	NumDownloads     int        `json:"numDownloads,omitempty"`   // Download count for ACS 'B' ratio
	MessagesPosted   int        `json:"messagesPosted,omitempty"` // Number of messages posted by user
	// NumLogons is TimesCalled
	TimeLimit   int    `json:"timeLimit"`   // Added for T (in minutes)
	PrivateNote string `json:"privateNote"` // Added for Z
	// Conference tracking for ACS codes C (message conference) and X (file conference)
	CurrentMsgConferenceID   int            `json:"current_msg_conference_id,omitempty"`
	CurrentMsgConferenceTag  string         `json:"current_msg_conference_tag,omitempty"`
	CurrentFileConferenceID  int            `json:"current_file_conference_id,omitempty"`
	CurrentFileConferenceTag string         `json:"current_file_conference_tag,omitempty"`
	GroupLocation            string         `json:"group_location,omitempty"`           // Added Group / Location field
	CurrentMessageAreaID     int            `json:"current_message_area_id,omitempty"`  // Added for default area tracking
	CurrentMessageAreaTag    string         `json:"current_message_area_tag,omitempty"` // Added for default area tracking
	LastReadMessageIDs       map[int]string `json:"last_read_message_ids,omitempty"`    // Map AreaID -> Last Read Message UUID string

	// File System Related
	CurrentFileAreaID  int         `json:"current_file_area_id,omitempty"`  // Added for default file area tracking
	CurrentFileAreaTag string      `json:"current_file_area_tag,omitempty"` // Added for default file area tracking
	TaggedFileIDs      []uuid.UUID `json:"tagged_file_ids,omitempty"`       // List of FileRecord IDs marked for batch download

	// Message System Related
	TaggedMessageAreaTags  []string `json:"tagged_message_area_tags,omitempty"` // List of message area tags tagged for newscan
	TaggedFileAreaIDs      []int    `json:"tagged_file_area_ids,omitempty"`     // File area IDs tagged for file newscan
	TaggedFileAreaTags     []string `json:"tagged_file_area_tags,omitempty"`    // File area tags tagged for file newscan config
	SeenNewscanNetworks    []string `json:"seen_newscan_networks,omitempty"`    // Lowercase network names already offered for newscan auto-join
	SeenNewscanAreaTags    []string `json:"seen_newscan_area_tags,omitempty"`   // Area tags already offered/added for newscan auto-join
	NewscanSeenInitialized bool     `json:"newscan_seen_initialized,omitempty"` // Seen-set tracking has been initialized for this user

	// System News tracking
	SeenNewsIDs         []int `json:"seen_news_ids,omitempty"`         // News item IDs already shown to this user (non-"always" items only)
	NewsSeenInitialized bool  `json:"news_seen_initialized,omitempty"` // SeenNewsIDs has been back-filled from PreviousLogin

	// Terminal Preferences
	ScreenWidth       int    `json:"screenWidth,omitempty"`       // Detected/preferred terminal width (default 80)
	ScreenHeight      int    `json:"screenHeight,omitempty"`      // Detected/preferred terminal height (default 25)
	PreferredEncoding string `json:"preferredEncoding,omitempty"` // User's encoding preference: "utf8", "cp437", or "" (not set)
	MsgHdr            int    `json:"msgHdr,omitempty"`            // Selected message header style; valid values are whatever MSGHDR.<n>.ans templates the menu set ships (0 = unset)

	// User Configuration Preferences
	HotKeys bool `json:"hotKeys,omitempty"`
	// MorePrompts is whether long output pauses a screenful at a time with
	// a More prompt. Unset (nil) is on; use MorePromptsOn. It was stored as
	// a plain bool that was never read, and never written as false, so an
	// existing record keeps the pauses it always had.
	MorePrompts     *bool  `json:"morePrompts,omitempty"`
	CustomPrompt    string `json:"customPrompt,omitempty"`
	OutputMode      string `json:"outputMode,omitempty"`
	FileListingMode string `json:"fileListingMode,omitempty"` // "lightbar" or "classic" (empty = server default)
	AutocorrectOff  bool   `json:"autocorrectOff,omitempty"`  // the caller turned the message editor's autocorrect off (Ctrl-O or User Konfig)
	FileListColumns struct {
		Name        bool `json:"name"`
		Size        bool `json:"size"`
		Date        bool `json:"date"`
		Downloads   bool `json:"downloads"`
		Uploader    bool `json:"uploader"`
		Description bool `json:"description"`
	} `json:"file_list_columns,omitempty"`
	AutoSignature string `json:"autoSignature,omitempty"` // Auto-signature appended to messages (max 5 lines)
	AutoSigAsk    bool   `json:"autoSigAsk,omitempty"`    // Ask before adding AutoSignature to each message
	Colors        [7]int `json:"colors,omitempty"`
	// ThreadViewClosed starts the message reader with its thread pane
	// closed; T in the reader still opens it for the rest of the call.
	ThreadViewClosed bool `json:"threadViewClosed,omitempty"`

	// V3Net Central consent. Each is off until the caller turns it on in
	// Central's My Preferences, unless a network's SysOp counts callers who
	// have not chosen as opted in (Callers Default). They only take effect
	// on networks where the SysOp offers the feature.
	//
	// CentralPresence shows the caller's handle on network Who's Online
	// while they are on, with CentralAvailability ("available", "busy" or
	// "" for not shared) beside it.
	CentralPresence     bool   `json:"centralPresence,omitempty"`
	CentralAvailability string `json:"centralAvailability,omitempty"`
	// CentralLastCallsSince is when the caller agreed to have their calls
	// shown on network Last Callers; nil is not agreed. Only calls that
	// began at or after it are published, so agreeing does not publish
	// calls made before, and clearing it withdraws every call at once.
	CentralLastCallsSince *time.Time `json:"centralLastCallsSince,omitempty"`
	// CentralPresenceChosen and CentralLastCallsChosen record that the
	// caller set that switch themselves. Until they have, a network whose
	// SysOp turned on Callers Default counts them as opted in there; see
	// CentralPresenceFor and CentralLastCallsFrom.
	CentralPresenceChosen  bool `json:"centralPresenceChosen,omitempty"`
	CentralLastCallsChosen bool `json:"centralLastCallsChosen,omitempty"`
	// CentralChat is the caller's agreement to take part in network chat,
	// given on the notice shown before their first network join or under
	// My Preferences: their handle and this board's name go to every
	// board in a room with each line they type. No Callers Default
	// applies; CentralChatChosen records that they answered the notice,
	// either way.
	CentralChat       bool `json:"centralChat,omitempty"`
	CentralChatChosen bool `json:"centralChatChosen,omitempty"`
	// CentralChatBarred is the SysOp's bar on this caller's network chat,
	// set in the user editor: barred, the caller cannot join or post on
	// any network from this board, whatever they agreed to. It is kept
	// with the consent fields, so a consent save on another node cannot
	// clear it.
	CentralChatBarred bool `json:"centralChatBarred,omitempty"`
	// CentralAlerts are the caller's V3Net Central activity alert choices.
	// They stand apart from the switches above: sharing presence or taking
	// part in chat does not share alerts or subscribe to them, and no
	// Callers Default applies.
	CentralAlerts CentralAlertPrefs `json:"centralAlerts,omitzero"`

	// Soft Delete (user marked as deleted but data preserved)
	DeletedUser bool       `json:"deletedUser,omitempty"` // True if user is soft-deleted
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`   // Timestamp when user was deleted (nil if not deleted)

	// New-user intro gate (requireNewUserEmail). IntroPending means the account
	// still owes the SysOp an introduction message; the login flow forces the
	// editor before the account can go any further, even below logon level.
	// IntroAttempts counts sessions where the caller reached that gate and left
	// without sending; the account is soft-deleted once it hits the limit.
	// Session-owned state (not carried in the sysop external-edit merge).
	IntroPending  bool `json:"introPending,omitempty"`
	IntroAttempts int  `json:"introAttempts,omitempty"`

	// APIDevices are the phones paired with the QWK API (see api_devices.go).
	// Owned by the BBS: ./ue does not edit them, so they are not part of the
	// external-edit merge. Only the UserMgr device methods change them, and
	// they always build a new slice, so copies of a User can share it safely.
	APIDevices []APIDevice `json:"apiDevices,omitempty"`
}

// CallRecord stores information about a single call session.
type CallRecord struct {
	UserID         int           `json:"userID"`
	Handle         string        `json:"handle"`
	GroupLocation  string        `json:"groupLocation,omitempty"`
	NodeID         int           `json:"nodeID"`
	ConnectTime    time.Time     `json:"connectTime"`
	DisconnectTime time.Time     `json:"disconnectTime"`
	Duration       time.Duration `json:"duration"`
	UploadedMB     float64       `json:"uploadedMB"`           // Placeholder for now
	DownloadedMB   float64       `json:"downloadedMB"`         // Placeholder for now
	Actions        string        `json:"actions"`              // Placeholder for now (e.g., "D,U,M")
	BaudRate       string        `json:"baudRate"`             // Static value for now
	CallNumber     uint64        `json:"callNumber,omitempty"` // Overall call number
	Invisible      bool          `json:"invisible,omitempty"`  // True if user was logged in invisibly
}

// MorePromptsOn reports whether u wants long output paused a screenful at a
// time: on unless the user has turned it off.
func (u *User) MorePromptsOn() bool {
	return u.MorePrompts == nil || *u.MorePrompts
}

// NoteRenamed records old as one of u's previous handles, once. Handles
// are compared as lastread records are keyed, by the lower-cased handle
// (jam lastread uses its CRC): "Sam" and "ſam" are one name to private mail
// but keep separate read pointers, so a rename between them is recorded.
func (u *User) NoteRenamed(old string) {
	old = strings.TrimSpace(old)
	key := strings.ToLower(old)
	if old == "" || key == strings.ToLower(strings.TrimSpace(u.Handle)) {
		return
	}
	for _, h := range u.PreviousHandles {
		if strings.ToLower(h) == key {
			return
		}
	}
	u.PreviousHandles = append(u.PreviousHandles, old)
}

// Handles is every handle u has had, the current one first.
func (u *User) Handles() []string {
	return append([]string{u.Handle}, u.PreviousHandles...)
}
