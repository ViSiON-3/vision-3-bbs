package user

import (
	"time"

	"github.com/google/uuid"
)

// MaxLastLogins = 10 // Max number of last logins to store (Moved to manager.go)

// LoginEvent holds information about a single login
type LoginEvent struct {
	Handle    string
	Timestamp time.Time
}

// User represents a user account.
type User struct {
	// gen tracks how many times this record has been refreshed from an
	// external edit. Sessions carry a copy taken at some point in the past; if
	// the authoritative record has since moved ahead, that copy must not be
	// allowed to write sysop-owned fields back. Unexported, so it is never
	// serialized to users.json.
	gen uint64

	ID             int       `json:"id"`           // Added User ID for ACS 'U' check
	PasswordHash   string    `json:"passwordHash"` // Changed from []byte to string
	Handle         string    `json:"handle"`
	LegacyUsername string    `json:"username,omitempty"` // Migration only: used during load when Handle is absent; cleared on save
	AccessLevel    int       `json:"accessLevel"`
	Flags          string    `json:"flags"`                // Added Flags string for ACS 'F' check (e.g., "XYZ")
	PublicKeys     []string  `json:"publicKeys,omitempty"` // OpenSSH authorized_keys lines authorized for WFC admin
	LastLogin      time.Time `json:"lastLogin"`
	// PreviousLogin is the LastLogin value from before the current session's
	// authentication. Authenticate() overwrites LastLogin with time.Now()
	// before the login sequence runs, so anything asking "what is new since
	// this user was last here?" (news, rumors, file newscan) must compare
	// against PreviousLogin, not LastLogin.
	PreviousLogin    time.Time `json:"previousLogin,omitempty"`
	TimesCalled      int       `json:"timesCalled"` // Used for E (NumLogons)
	LastBulletinRead time.Time `json:"lastBulletinRead"`
	RealName         string    `json:"realName"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"` // For optimistic locking - tracks last modification
	Validated        bool      `json:"validated"`
	FilePoints       int       `json:"filePoints"`               // Added for P
	NumUploads       int       `json:"numUploads"`               // Added for E
	NumDownloads     int       `json:"numDownloads,omitempty"`   // Download count for ACS 'B' ratio
	MessagesPosted   int       `json:"messagesPosted,omitempty"` // Number of messages posted by user
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
	MsgHdr            int    `json:"msgHdr,omitempty"`            // Selected message header style (1-14, 0=unset)

	// User Configuration Preferences
	HotKeys         bool   `json:"hotKeys,omitempty"`
	MorePrompts     bool   `json:"morePrompts,omitempty"`
	CustomPrompt    string `json:"customPrompt,omitempty"`
	OutputMode      string `json:"outputMode,omitempty"`
	FileListingMode string `json:"fileListingMode,omitempty"` // "lightbar" or "classic" (empty = server default)
	FileListColumns struct {
		Name        bool `json:"name"`
		Size        bool `json:"size"`
		Date        bool `json:"date"`
		Downloads   bool `json:"downloads"`
		Uploader    bool `json:"uploader"`
		Description bool `json:"description"`
	} `json:"file_list_columns,omitempty"`
	AutoSignature string `json:"autoSignature,omitempty"` // Auto-signature appended to messages (max 5 lines)
	Colors        [7]int `json:"colors,omitempty"`

	// Soft Delete (user marked as deleted but data preserved)
	DeletedUser bool       `json:"deletedUser,omitempty"` // True if user is soft-deleted
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`   // Timestamp when user was deleted (nil if not deleted)
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
