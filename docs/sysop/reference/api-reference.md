# ViSiON/3 API Reference

This document provides a reference for the main packages, interfaces, scripting APIs, and data structures in ViSiON/3.

## Package Overview

```text
internal/
├── ansi/           # ANSI and pipe code processing
├── archiver/       # Archive format registry (ZIP, RAR, 7Z, ARJ, LHA)
├── chat/           # Inter-node teleconference chat
├── conference/     # Message/file area conference grouping
├── config/         # Configuration loading and structures
├── configeditor/   # System configuration editor TUI
├── editor/         # Text editor implementation
├── file/           # File area management
├── ftn/            # FTN packet (.PKT) library
├── jam/            # JAM message base implementation
├── menu/           # Menu system execution
├── menueditor/     # Menu and command editor TUI
├── message/        # Message area management (JAM-backed)
├── qwk/            # QWK mail packet reader/writer
├── scheduler/      # Cron-based event scheduler
├── scripting/      # Vision/3 native JavaScript runtime (V3 API)
├── session/        # Session state management
├── sshserver/      # SSH server with legacy algorithm support
├── stringeditor/   # String resource editor TUI
├── syncjs/         # Synchronet-compatible JavaScript runtime
├── telnetserver/   # Telnet server (RFC 854)
├── terminalio/     # Terminal I/O with encoding support
├── tosser/         # FTN echomail/netmail toss engine
├── transfer/       # File transfer protocols (ZMODEM/YMODEM/XMODEM)
├── types/          # Shared type definitions
├── user/           # User management
├── usereditor/     # User account browser/editor TUI
├── util/           # Utility helpers
├── v3net/          # V3Net inter-BBS networking
├── version/        # Build version constant
└── ziplab/         # Archive upload processing pipeline
```

---

## Core Packages

### user

User management and authentication.

```go
type UserMgr struct { /* Internal fields */ }

func NewUserManager(dataPath string) (*UserMgr, error)
func (um *UserMgr) Authenticate(username, password string) (*User, bool)
func (um *UserMgr) GetUser(username string) (*User, bool)
func (um *UserMgr) AddUser(password, handle, realName, groupLocation string) (*User, error)
func (um *UserMgr) SaveUsers() error
func (um *UserMgr) GetAllUsers() []*User
func (um *UserMgr) AddCallRecord(record CallRecord)
func (um *UserMgr) GetLastCallers() []CallRecord
```

### menu

Menu system loading and execution.

```go
type MenuExecutor struct {
    MenuSetPath    string
    RootConfigPath string
    RootAssetsPath string
    RunRegistry    map[string]RunnableFunc
    DoorRegistry   map[string]config.DoorConfig
    OneLiners      []string
    LoadedStrings  config.StringsConfig
    Theme          config.ThemeConfig
    MessageMgr     *message.MessageManager
    FileMgr        *file.FileManager
}

func NewExecutor(menuSetPath, rootConfigPath, rootAssetsPath string, ...) *MenuExecutor
func (e *MenuExecutor) Run(s ssh.Session, terminal *term.Terminal, ...) (string, *User, error)
```

### message

Message area management backed by JAM message bases.

```go
type MessageManager struct { /* Internal fields */ }

func NewMessageManager(dataPath, configPath, boardName string) (*MessageManager, error)
func (m *MessageManager) Close() error
func (m *MessageManager) ListAreas() []*MessageArea
func (m *MessageManager) GetAreaByID(id int) (*MessageArea, bool)
func (m *MessageManager) GetAreaByTag(tag string) (*MessageArea, bool)
func (m *MessageManager) AddMessage(areaID int, from, to, subject, body, replyMsgID string) (int, error)
func (m *MessageManager) GetMessage(areaID, msgNum int) (*DisplayMessage, error)
func (m *MessageManager) GetMessageCountForArea(areaID int) (int, error)
func (m *MessageManager) GetNewMessageCount(areaID int, username string) (int, error)
func (m *MessageManager) GetLastRead(areaID int, username string) (int, error)
func (m *MessageManager) SetLastRead(areaID int, username string, msgNum int) error
func (m *MessageManager) GetNextUnreadMessage(areaID int, username string) (int, error)
func (m *MessageManager) GetBase(areaID int) (*jam.Base, error)
```

### jam

JAM (Joaquim-Andrew-Mats) binary message base implementation.

```go
type Base struct { /* Thread-safe with sync.RWMutex */ }

func Open(basePath string) (*Base, error)
func Create(basePath string) (*Base, error)
func (b *Base) Close() error
func (b *Base) GetMessageCount() (int, error)
func (b *Base) ReadMessage(msgNum int) (*Message, error)
func (b *Base) ReadMessageHeader(msgNum int) (*MessageHeader, error)
func (b *Base) WriteMessage(msg *Message) (int, error)
func (b *Base) WriteMessageExt(msg *Message, msgType MessageType, echoTag, boardName string) (int, error)
func (b *Base) UpdateMessageHeader(msgNum int, hdr *MessageHeader) error
func (b *Base) DeleteMessage(msgNum int) error
func (b *Base) GetLastRead(username string) (int, error)
func (b *Base) SetLastRead(username string, msgNum int) error
func (b *Base) GetNextUnreadMessage(username string) (int, error)
func (b *Base) GetUnreadCount(username string) (int, error)

// Address handling
func ParseAddress(s string) (*FidoAddress, error)
func (a *FidoAddress) String() string   // 4D: "Z:N/N.P"
func (a *FidoAddress) String2D() string // 2D: "N/N" (for SEEN-BY/PATH)
```

### ftn

FidoNet Technology Network Type-2+ packet library.

```go
type PacketHeader struct { /* 58-byte .PKT header */ }
type PackedMessage struct {
    MsgType, OrigNode, DestNode, OrigNet, DestNet, Attr uint16
    DateTime, To, From, Subject, Body string
}
type ParsedBody struct {
    Area string; Kludges []string; Text string; SeenBy, Path []string
}

func NewPacketHeader(origZone, origNet, origNode, origPoint,
    destZone, destNet, destNode, destPoint uint16, password string) *PacketHeader
func ReadPacket(r io.Reader) (*PacketHeader, []*PackedMessage, error)
func WritePacket(w io.Writer, hdr *PacketHeader, msgs []*PackedMessage) error
func ParsePackedMessageBody(body string) *ParsedBody
func FormatPackedMessageBody(parsed *ParsedBody) string
func FormatFTNDateTime(t time.Time) string
func ParseFTNDateTime(s string) (time.Time, error)
```

### file

File area management.

```go
type FileManager struct { /* Internal fields */ }

func NewFileManager(dataPath, configPath string) (*FileManager, error)
func (fm *FileManager) GetAllAreas() []FileArea
func (fm *FileManager) GetAreaByID(id int) (*FileArea, error)
func (fm *FileManager) GetFilesForArea(areaID int) ([]FileRecord, error)
```

### ansi

ANSI escape sequence and pipe code processing.

```go
func ReplacePipeCodes(data []byte) []byte
func ProcessAnsiAndExtractCoords(rawContent []byte, outputMode OutputMode) (ProcessAnsiResult, error)
func GetAnsiFileContent(filename string) ([]byte, error)
func ClearScreen() string
func MoveCursor(row, col int) string
func StripAnsi(str string) string

type ProcessAnsiResult struct {
    DisplayBytes []byte                        // Processed ANSI content ready for display
    FieldCoords  map[string]struct{ X, Y int } // Field codes (|P, |O, etc.) → coordinates
    FieldColors  map[string]string             // Field codes → cumulative ANSI color sequences
}

const (
    OutputModeAuto  OutputMode = iota
    OutputModeUTF8
    OutputModeCP437
)

// FieldColors captures cumulative SGR state (Select Graphic Rendition):
// - Bold, dim, italic, underline, blink, reverse, hidden attributes
// - Foreground colors (30-37 normal, 90-97 bright)
// - Background colors (40-47 normal, 100-107 bright)
// Example: ESC[1m ESC[35m ESC[44m → FieldColors["P"] = "ESC[1;35;44m"
```

### conference

Message and file area conference grouping with ACS support.

```go
type Conference struct {
    ID          int
    Position    int      // Sort order
    Tag         string
    Name        string
    Description string
    ACS         string
    AllowAnon   *bool
}

type ConferenceManager struct { /* Internal fields */ }

func NewConferenceManager(configPath string) (*ConferenceManager, error)
func (cm *ConferenceManager) GetByID(id int) (*Conference, bool)
func (cm *ConferenceManager) GetByTag(tag string) (*Conference, bool)
func (cm *ConferenceManager) ListConferences() []*Conference
```

### archiver

Centralized archive format registry loaded from `archivers.json`.

```go
type Archiver struct {
    ID         string
    Extension  string
    Magic      []byte      // Magic bytes for format detection
    Native     bool        // Use Go stdlib (ZIP only)
    Enabled    bool
    // Command templates for pack/unpack/test/list/comment operations
    PackCmd, UnpackCmd, TestCmd, ListCmd, CommentCmd CommandDef
}

type CommandDef struct {
    Command string
    Args    []string   // Placeholders: {ARCHIVE}, {FILES}, {OUTDIR}, {WORKDIR}
}

func DefaultConfig() Config
func (a *Archiver) MatchesExtension(filename string) bool
```

### chat

Global teleconference chat room with message broadcasting.

```go
type ChatRoom struct { /* Thread-safe subscriber management */ }
type ChatMessage struct {
    NodeID    int
    Handle    string
    Text      string
    Timestamp time.Time
    IsSystem  bool
}

func NewChatRoom(maxHistory int) *ChatRoom
func (cr *ChatRoom) Subscribe(nodeID int, handle string) <-chan ChatMessage
func (cr *ChatRoom) Unsubscribe(nodeID int)
func (cr *ChatRoom) Broadcast(senderNodeID int, handle string, text string)
func (cr *ChatRoom) BroadcastSystem(text string)
func (cr *ChatRoom) History() []ChatMessage
func (cr *ChatRoom) ActiveCount() int
```

### qwk

QWK packet reader/writer for offline mail exchanges.

```go
type ConferenceInfo struct {
    Number int
    Name   string
}

type PacketMessage struct {
    Conference int
    Number     int
    From, To, Subject string
    DateTime   time.Time
    Body       string
    Private    bool
}

type REPMessage struct {
    Conference int
    To, Subject, Body string
}

func ReadREP(r io.ReaderAt, size int64, bbsID string) ([]REPMessage, error)

const (
    BlockSize     = 128
    StatusPublic  = ' '
    StatusPrivate = '*'
)
```

### scheduler

Cron-based event scheduler with concurrency control.

```go
type Scheduler struct { /* Internal cron engine and history */ }

type EventHistory struct {
    EventID      string
    LastRun      time.Time
    LastStatus   string
    RunCount     int
    SuccessCount int
    FailureCount int
}

type EventResult struct {
    EventID   string
    StartTime time.Time
    EndTime   time.Time
    Success   bool
    ExitCode  int
    Output    string
    Error     error
}

func NewScheduler(cfg config.EventsConfig, historyPath string) *Scheduler
func (s *Scheduler) Start(ctx context.Context)
func (s *Scheduler) Stop()
```

### tosser

FTN echomail/netmail toss engine with duplicate detection.

```go
type Tosser struct { /* Per-network toss engine */ }

type TossResult struct {
    PacketsProcessed  int
    MessagesImported  int
    MessagesExported  int
    DupesSkipped      int
    Errors            []error
}

type DupeDB struct { /* Message ID duplicate tracker */ }

func New(networkName string, cfg networkConfig, globalCfg config.FTNConfig,
    dupeDB *DupeDB, msgMgr *message.MessageManager) *Tosser
func NewDupeDBFromPath(dupeDBPath string) (*DupeDB, error)
func (t *Tosser) Start(ctx context.Context)
func (t *Tosser) RunOnce() TossResult
func (t *Tosser) ProcessInbound() error
func (t *Tosser) ScanAndExport() error
func (t *Tosser) PurgeDupes()
```

### transfer

File transfer protocol management (ZMODEM, YMODEM, XMODEM).

```go
type ProtocolConfig struct {
    Key, Name, Description string
    SendCmd, RecvCmd       string
    SendArgs, RecvArgs     []string
    BatchSend              bool
    UsePTY                 bool
    Default                bool
    ConnectionType         string // "", "ssh", "telnet"
}

func LoadProtocols(path string) ([]ProtocolConfig, error)
func RunCommandDirect(ctx context.Context, s ssh.Session,
    cmd *exec.Cmd, stdinIdleTimeout time.Duration) error

const (
    ConnTypeAny    = ""
    ConnTypeSSH    = "ssh"
    ConnTypeTelnet = "telnet"
)
// Argument placeholders: {filePath}, {fileListPath}, {targetDir}
```

### sshserver

Pure-Go SSH server with legacy algorithm support for retro BBS clients.

```go
type Server struct { /* gliderlabs/ssh wrapper */ }

type Config struct {
    HostKeyPath                  string
    Host                         string
    Port                         int
    LegacySSHAlgorithms          bool
    SessionHandler               func(ssh.Session)
    PasswordHandler              func(ctx ssh.Context, password string) bool
    KeyboardInteractiveHandler   func(...)
}

type BBSSession struct { /* Wraps ssh.Session with read interrupt and raw write */ }

func NewServer(cfg Config) (*Server, error)
func (s *Server) ListenAndServe() error
func (s *Server) Close() error
func WrapSession(s ssh.Session) *BBSSession
func (bs *BBSSession) RawWrite(p []byte) (int, error)        // Binary transfer bypass
func (bs *BBSSession) SetReadInterrupt(ch <-chan struct{})
func (bs *BBSSession) SetTransferActive(active bool)

var ErrReadInterrupted = errors.New("read interrupted")
```

### telnetserver

Pure TCP telnet server implementing RFC 854.

```go
type Server struct { /* TCP listener */ }
type Config struct {
    Port           int
    Host           string
    SessionHandler func(ssh.Session) // Adapts to same interface as SSH
}

type TelnetConn struct { /* IAC state machine wrapper */ }
type TelnetSessionAdapter struct { /* Adapts TelnetConn to ssh.Session */ }

func NewServer(cfg Config) *Server
func (s *Server) ListenAndServe() error
func (s *Server) Close() error
func NewTelnetConn(conn net.Conn) *TelnetConn
func (tc *TelnetConn) Negotiate() error // Option negotiation (NAWS, terminal type)
func NewTelnetSessionAdapter(tc *TelnetConn) *TelnetSessionAdapter

const (
    IAC  = 255
    OptEcho     = 1
    OptSGA      = 3
    OptTermType = 24
    OptNAWS     = 31
    OptLinemode = 34
)
```

### v3net

V3Net inter-BBS networking service.

```go
type Service struct { /* Hub, leaves, dedup, keystore coordinator */ }
type JAMWriter interface { /* Interface for writing messages to JAM bases */ }

func New(cfg config.V3NetConfig) (*Service, error)
func (s *Service) AddLeaf(lcfg config.V3NetLeafConfig, writer JAMWriter,
    onEvent func(protocol.Event)) error
func (s *Service) Start(ctx context.Context) error
func (s *Service) Close() error
func (s *Service) NodeID() string
func (s *Service) SendMessage(network string, msg protocol.Message) error
func (s *Service) SendLogon(handle string)
func (s *Service) SendLogoff(handle string)
func (s *Service) HubActive() bool
func (s *Service) LeafCount() int
func (s *Service) LeafNetworks() []string
func (s *Service) RegisterArea(areaID int, network string)
func (s *Service) NetworkForArea(areaID int) (string, bool)
```

**Sub-packages:** `protocol/` (message types, validation), `hub/` (hub server), `leaf/` (leaf client), `keystore/` (node keys), `dedup/` (dedup index), `registry/` (area/network mapping).

### scripting

Vision/3 native JavaScript runtime (goja) for user scripts.

```go
type Engine struct { /* goja runtime, session context, timeout control */ }

type SessionContext struct {
    NodeNumber  int
    Handle      string
    UserID      int
    // TTY, terminal dimensions, etc.
}

type ScriptConfig struct {
    WorkingDir     string
    MaxRunTime     time.Duration
    SandboxedPaths []string
}

type Providers struct {
    UserMgr    *user.UserMgr
    MessageMgr *message.MessageManager
    FileMgr    *file.FileManager
    // Additional provider references
}

func NewEngine(ctx context.Context, session *SessionContext,
    cfg ScriptConfig, providers *Providers) *Engine
func (e *Engine) Run(scriptPath string) error
func (e *Engine) Close()
```

### syncjs

Synchronet-compatible JavaScript runtime (goja) for legacy JS doors.

```go
type Engine struct { /* goja runtime, module stack, exit handlers, I/O */ }

type SessionContext struct {
    NodeNumber int
    Handle     string
    // Session data
}

type SyncJSDoorConfig struct {
    WorkingDir   string
    Script       string
    LibraryPaths []string
    ExecDir      string
}

func NewEngine(ctx context.Context, session *SessionContext,
    cfg SyncJSDoorConfig) *Engine
func (e *Engine) Run(scriptPath string) error
func (e *Engine) Close()
```

### ziplab

Archive upload processing pipeline (virus scan, integrity, ads removal).

```go
type Processor struct { /* 7-step pipeline executor */ }

type Config struct {
    Enabled         bool
    RunOnUpload     bool
    ScanFailBehavior string
    Steps           StepsConfig
    ArchiveTypes    []ArchiveType
}

func NewProcessor(cfg Config, baseDir string) *Processor
func DefaultConfig() Config
func LoadConfig(configPath string) (Config, error)
func (p *Processor) StepTestIntegrity(archivePath string) error
func (p *Processor) StepExtract(archivePath string) (string, error)
```

**Pipeline Steps:** Test Integrity → Extract to Temp → Virus Scan → Remove Ads → Add Comment → Include File → Repack.

### config

Configuration structures and loading.

```go
type ServerConfig struct {
    BoardName, SysOpName, Timezone         string
    SSHHost, TelnetHost                    string
    SysOpLevel, CoSysOpLevel               int
    InvisibleLevel, NewUserLevel           int
    RegularUserLevel, LogonLevel           int
    AnonymousLevel                         int
    SSHPort, TelnetPort                    int
    MaxNodes, MaxConnectionsPerIP          int
    IPBlocklistPath, IPAllowlistPath       string
    MaxFailedLogins, LockoutMinutes        int
    FileListingMode                        string
    LegacySSHAlgorithms, AllowNewUsers     bool
    SessionIdleTimeoutMinutes              int
    TransferTimeoutMinutes                 int
    DeletedUserRetentionDays               int
    UseNUV, AutoAddNUV, NUVValidate        bool
    NUVKill                                bool
    NUVUseLevel, NUVYesVotes, NUVNoVotes   int
    NUVLevel, NUVForm                      int
    DosemuPath                             string
}

type DoorConfig struct {
    Name                string
    Commands            []string
    WorkingDirectory    string
    DropfileType        string              // DOOR.SYS, DOOR32.SYS, CHAIN.TXT, DORINFO1.DEF
    DropfileLocation    string              // "startup" or "node"
    IOMode              string              // "STDIO" or "SOCKET"
    RequiresRawTerminal bool
    UseShell            bool
    SingleInstance      bool
    MinAccessLevel      int
    CleanupCommand      string
    CleanupArgs         []string
    EnvironmentVars     map[string]string
    // Script door fields
    Type                string              // "synchronet_js" or "v3_script"
    Script              string
    LibraryPaths        []string
    ExecDir             string
    Args                []string
    // DOS door fields
    IsDOS               bool
    DriveCPath          string
    DOSEmulator         string
    FossilDriver        string
    DosemuConfig        string
}

type LoginItem struct {
    Command     string
    Data        string
    ClearScreen bool
    PauseAfter  bool
    SecLevel    int
}

type ThemeConfig struct {
    YesNoHighlightColor int
    YesNoRegularColor   int
}

type StringsConfig struct { /* 500+ user-facing string fields */ }

type FTNConfig struct {
    DupeDBPath, InboundPath, SecureInboundPath string
    OutboundPath, BinkdOutboundPath            string
    TempPath, BadAreaTag, DupeAreaTag           string
    Networks map[string]FTNNetworkConfig
}

type FTNNetworkConfig struct {
    InternalTosserEnabled bool
    OwnAddress            string
    PollSeconds           int
    Tearline              string
    Links                 []FTNLinkConfig
}

type FTNLinkConfig struct {
    Address, PacketPassword string
    AreafixPassword, Name   string
    Flavour                 string
}

type V3NetConfig struct {
    Enabled      bool
    KeystorePath string
    DedupDBPath  string
    RegistryURL  string
    Hub          V3NetHubConfig
    Leaves       []V3NetLeafConfig
}

type V3NetHubConfig struct {
    Enabled     bool
    ListenAddr  string
    TLSCert     string
    TLSKey      string
    DataDir     string
    AutoApprove bool
    Networks    []V3NetHubNetwork
}

type V3NetHubNetwork struct {
    Name, Description string
}

type V3NetLeafConfig struct {
    HubURL, Network, Board string
    PollInterval, Origin   string
}

type EventsConfig struct {
    Enabled             bool
    MaxConcurrentEvents int
    Events              []EventConfig
}

type EventConfig struct {
    ID, Name, Schedule, Command string
    Args                        []string
    WorkingDirectory            string
    TimeoutSeconds              int
    Enabled, RunAtStartup       bool
    EnvironmentVars             map[string]string
    RunAfter                    string
    DelayAfterSeconds           int
}

// Key functions:
func LoadStrings(configPath string) (StringsConfig, error)
func LoadDoors(path string) (map[string]DoorConfig, error)
func LoadThemeConfig(menuSetPath string) (ThemeConfig, error)
```

### terminalio

Terminal I/O with encoding support.

```go
func WriteProcessedBytes(terminal io.Writer, processedBytes []byte, outputMode ansi.OutputMode) error
```

### types

Shared type definitions.

```go
type AutoRunTracker map[string]bool
```

### version

Build version constant.

```go
var Number string = "0.1.0"
// Override at build time: -ldflags "-X github.com/ViSiON-3/vision-3-bbs/internal/version.Number=X.Y.Z"
```

### util

Utility helpers.

```go
func FormatFileSize(size int64) string // Human-readable: bytes, KB, MB, GB
```

### TUI Editors

The following packages provide BubbleTea-based TUI editors recreating the original Vision/2 Turbo Pascal utilities:

| Package        | Original     | Purpose                                                            |
| -------------- | ------------ | ------------------------------------------------------------------ |
| `configeditor` | CONFED.EXE   | System config, message areas, file areas, conferences, FTN, V3Net  |
| `menueditor`   | MENUEDIT.EXE | Menu definitions and command editing                               |
| `stringeditor` | STRING.EXE   | User-facing string/prompt editing with search and factory defaults |
| `usereditor`   | USEREDIT.EXE | User account browser/editor with mass operations                   |

---

## Data Structures

### User

```go
type User struct {
    ID                       int
    PasswordHash             string
    Handle                   string
    LegacyUsername           string
    AccessLevel              int
    Flags                    string
    LastLogin                time.Time
    TimesCalled              int
    LastBulletinRead         time.Time
    RealName                 string
    CreatedAt                time.Time
    UpdatedAt                time.Time
    Validated                bool
    FilePoints               int
    NumUploads               int
    NumDownloads             int
    MessagesPosted           int
    TimeLimit                int
    PrivateNote              string
    CurrentMsgConferenceID   int
    CurrentMsgConferenceTag  string
    CurrentFileConferenceID  int
    CurrentFileConferenceTag string
    GroupLocation            string
    CurrentMessageAreaID     int
    CurrentMessageAreaTag    string
    LastReadMessageIDs       map[int]string
    CurrentFileAreaID        int
    CurrentFileAreaTag       string
    TaggedFileIDs            []uuid.UUID
    TaggedMessageAreaTags    []string
    TaggedFileAreaTags       []string
    ScreenWidth              int
    ScreenHeight             int
    PreferredEncoding        string
    MsgHdr                   int
    HotKeys                  bool
    MorePrompts              bool
    CustomPrompt             string
    OutputMode               string
    FileListingMode          string
    FileListColumns          struct {
        Name, Size, Date, Downloads, Uploader, Description bool
    }
    AutoSignature            string
    Colors                   [7]int
    DeletedUser              bool
    DeletedAt                *time.Time
}
```

### CallRecord

```go
type CallRecord struct {
    UserID         int
    Handle         string
    GroupLocation  string
    NodeID         int
    ConnectTime    time.Time
    DisconnectTime time.Time
    Duration       time.Duration
    UploadedMB     float64
    DownloadedMB   float64
    Actions        string
    BaudRate       string
    CallNumber     uint64
    Invisible      bool
}
```

### AdminActivityLog

```go
type AdminActivityLog struct {
    ID            int
    Timestamp     time.Time
    AdminHandle   string
    AdminID       int
    TargetUserID  int
    TargetHandle  string
    Action        string
    FieldName     string
    OldValue      string
    NewValue      string
    Notes         string
}
```

### MessageArea

```go
type MessageArea struct {
    ID           int    `json:"id"`
    Position     int    `json:"position"`
    Tag          string `json:"tag"`
    Name         string `json:"name"`
    Description  string `json:"description"`
    ACSRead      string `json:"acs_read"`
    ACSWrite     string `json:"acs_write"`
    AllowAnon    *bool  `json:"allow_anon"`
    RealNameOnly bool   `json:"real_name_only"`
    ConferenceID int    `json:"conference_id"`
    BasePath     string `json:"base_path"`
    MaxMessages  int    `json:"max_messages"`
    MaxAge       int    `json:"max_age"`
    AutoJoin     bool   `json:"auto_join"`
    AreaType     string `json:"area_type"`       // "local", "echomail", "netmail"
    EchoTag      string `json:"echo_tag"`
    OriginAddr   string `json:"origin_addr"`
    Network      string `json:"network"`
    Sponsor      string `json:"sponsor"`
}
```

### DisplayMessage

```go
type DisplayMessage struct {
    MsgNum     int
    From       string
    To         string
    Subject    string
    DateTime   time.Time
    Body       string
    MsgID      string
    ReplyID    string
    ReplyToNum int
    OrigAddr   string
    DestAddr   string
    Attributes uint32
    IsPrivate  bool
    IsDeleted  bool
    AreaID     int
}
```

### FileArea

```go
type FileArea struct {
    ID            int
    Tag           string
    Name          string
    Description   string
    Path          string
    ACSList       string
    ACSUpload     string
    ACSDownload   string
    ConferenceID  int
}
```

### FileRecord

```go
type FileRecord struct {
    ID            uuid.UUID
    AreaID        int
    Filename      string
    Description   string
    Size          int64
    UploadedAt    time.Time
    UploadedBy    string
    DownloadCount int
    Reviewed      bool
}
```

### JAM Structures

```go
type FixedHeaderInfo struct {
    Signature   [4]byte
    DateCreated uint32
    ModCounter  uint32
    ActiveMsgs  uint32
    PasswordCRC uint32
    BaseMsgNum  uint32
    Reserved    [1000]byte
}

type MessageHeader struct {
    Signature                              [4]byte
    Revision, ReservedWord                 uint16
    SubfieldLen                            uint32
    TimesRead, MSGIDcrc, REPLYcrc          uint32
    ReplyTo, Reply1st, ReplyNext           uint32
    DateWritten, DateReceived, DateProcessed uint32
    MessageNumber                          uint32
    Attribute, Attribute2                   uint32
    Offset, TxtLen                         uint32
    PasswordCRC, Cost                      uint32
    Subfields                              []Subfield
}

type Subfield struct {
    LoID, HiID uint16
    DatLen     uint32
    Buffer     []byte
}

type IndexRecord struct {
    ToCRC, HdrOffset uint32
}

type LastReadRecord struct {
    UserCRC, UserID, LastReadMsg, HighReadMsg uint32
}

type Message struct {
    Header                      *MessageHeader
    From, To, Subject           string
    DateTime                    time.Time
    Text                        string
    OrigAddr, DestAddr          string
    MsgID, ReplyID, PID, Flags  string
    SeenBy, Path                string
    Kludges                     []string
}
```

---

## Menu System

### Menu Command Types

| Command  | Syntax                    | Description                            |
| -------- | ------------------------- | -------------------------------------- |
| `GOTO`   | `GOTO:MENUNAME`           | Navigate to another menu               |
| `RUN`    | `RUN:FUNCTIONNAME [ARGS]` | Execute a registered runnable function |
| `DOOR`   | `DOOR:DOORNAME`           | Launch an external door program        |
| `LOGOFF` | `LOGOFF`                  | Disconnect the user session            |

### Command Record Structure

```json
{
    "KEYS": "M",
    "CMD": "RUN:READMSGS",
    "ACS": "s10",
    "HIDDEN": false,
    "AUTORUN": "ONCE_PER_SESSION",
    "NODE_ACTIVITY": "Reading Messages"
}
```

- **KEYS** — Hotkey(s) to trigger the command
- **CMD** — Command string (GOTO:/RUN:/DOOR:/LOGOFF)
- **ACS** — Access Control String for security validation
- **HIDDEN** — Whether command appears in help/menu display
- **AUTORUN** — Auto-run type (e.g., `"ONCE_PER_SESSION"`)
- **NODE_ACTIVITY** — Activity text shown in "Who's Online"

### Login Sequence

Configurable via `login.json`. Each entry defines:

```go
type LoginItem struct {
    Command     string  // RUN: target
    Data        string  // Optional command-specific data
    ClearScreen bool    // Auto-clear before item
    PauseAfter  bool    // Auto-pause after item
    SecLevel    int     // Minimum access level required
}
```

### Runnable Functions

#### User Management

| Function              | Description                                         |
| --------------------- | --------------------------------------------------- |
| `AUTHENTICATE`        | User login (pre-login screen)                       |
| `NEWUSER`             | New user application process                        |
| `VALIDATEUSER`        | Validate user accounts (admin)                      |
| `NEWUSERVAL`          | Prompt to validate new users if pending             |
| `UNVALIDATEUSER`      | Remove validation from user accounts                |
| `BANUSER`             | Quick-ban user accounts                             |
| `DELETEUSER`          | Soft-delete user accounts                           |
| `PURGEUSERS`          | Permanently purge soft-deleted users past retention |
| `ADMINLISTUSERS`      | Admin detailed user browser                         |
| `TOGGLEALLOWNEWUSERS` | Toggle new user registration                        |
| `LISTUSERS`           | Display user list                                   |

#### Login Sequence

| Function                  | Description                                |
| ------------------------- | ------------------------------------------ |
| `FULL_LOGIN_SEQUENCE`     | Complete login flow                        |
| `FASTLOGIN`               | Inline fast login menu                     |
| `PRINTNEWS`               | Display news since last login              |
| `PENDINGVALIDATIONNOTICE` | SysOp notice for users awaiting validation |

#### System Information

| Function      | Description                     |
| ------------- | ------------------------------- |
| `SHOWSTATS`   | Display user statistics         |
| `SYSTEMSTATS` | Display system statistics       |
| `LASTCALLERS` | Show recent callers             |
| `SHOWVERSION` | Show system version             |
| `V3NETSTATUS` | V3Net networking status display |

#### Messages

| Function                  | Description                                |
| ------------------------- | ------------------------------------------ |
| `LISTMSGAR`               | List message areas (grouped by conference) |
| `SELECTMSGAREA`           | Select message area (lightbar UI)          |
| `COMPOSEMSG`              | Compose new message in current area        |
| `PROMPTANDCOMPOSEMESSAGE` | Select area then compose                   |
| `READMSGS`                | Read messages (full reader UI)             |
| `LISTMSGS`                | List messages in current area              |
| `NEWSCAN`                 | Scan for new messages                      |
| `NEWSCANCONFIG`           | Configure personal newscan tagged areas    |
| `UPDATENEWSCAN`           | Update newscan pointers to specific date   |
| `SENDPRIVMAIL`            | Send private mail                          |
| `READPRIVMAIL`            | Read private mail                          |
| `LISTPRIVMAIL`            | List private mail                          |
| `NMAILSCAN`               | New mail scan (unread only)                |
| `CHANGEMSGCONF`           | Change message conference (lightbar)       |
| `GETHEADERTYPE`           | Message header style selection             |
| `NEXTMSGAREA`             | Navigate to next message area              |
| `PREVMSGAREA`             | Navigate to previous message area          |
| `NEXTMSGCONF`             | Navigate to next conference                |
| `PREVMSGCONF`             | Navigate to previous conference            |

#### Message Sponsorship

| Function          | Description                      |
| ----------------- | -------------------------------- |
| `SPONSORMENU`     | Sponsor menu                     |
| `SPONSOREDITAREA` | Edit current message area fields |

#### Files

| Function             | Description                                              |
| -------------------- | -------------------------------------------------------- |
| `LISTFILES`          | Display file list (format per user preference)           |
| `LISTFILES_EXTENDED` | Extended file listing (all columns)                      |
| `LISTFILEAR`         | List file areas                                          |
| `SELECTFILEAREA`     | File area selection                                      |
| `VIEW_FILE`          | View/display file (archives show listing, text is paged) |
| `TYPE_TEXT_FILE`     | Type text file with paging                               |
| `SHOWFILEINFO`       | Show file metadata                                       |
| `UPLOADFILE`         | ZMODEM file upload                                       |
| `DOWNLOADFILE`       | V2-style download: prompt, add to batch, transfer        |
| `BATCHDOWNLOAD`      | Download tagged batch files                              |
| `CLEAR_BATCH`        | Clear tagged file batch queue                            |
| `SEARCH_FILES`       | Search files across all areas                            |
| `FILE_NEWSCAN`       | Scan file areas for new uploads                          |
| `EDITFILERECORD`     | SysOp file review queue                                  |
| `WANTLIST`           | File want list                                           |
| `FILENEWSCANCONFIG`  | File newscan area config                                 |
| `CFG_FILECOLUMNS`    | Configure file listing columns                           |

#### Doors

| Function      | Description                                       |
| ------------- | ------------------------------------------------- |
| `LISTDOORS`   | List available doors with access control          |
| `OPENDOOR`    | Prompt and open a door                            |
| `DOORINFO`    | Show door information                             |
| `RUNDOOR`     | Run external script/door (login sequence variant) |
| `DISPLAYFILE` | Display ANSI file (login sequence variant)        |

#### User Configuration

| Function           | Description                              |
| ------------------ | ---------------------------------------- |
| `CFG_HOTKEYS`      | Configure hotkey settings                |
| `CFG_MOREPROMPTS`  | Configure more prompt behavior           |
| `CFG_SCREENWIDTH`  | Set screen width                         |
| `CFG_SCREENHEIGHT` | Set screen height                        |
| `CFG_TERMTYPE`     | Set terminal type                        |
| `CFG_REALNAME`     | Edit real name                           |
| `CFG_NOTE`         | Edit personal note                       |
| `CFG_CUSTOMPROMPT` | Configure custom prompt                  |
| `CFG_COLOR`        | Configure color output                   |
| `CFG_PASSWORD`     | Change password                          |
| `CFG_FILELISTMODE` | Set file listing mode (lightbar/classic) |
| `CFG_AUTOSIG`      | Configure auto-signature                 |
| `CFG_VIEWCONFIG`   | View user configuration summary          |

#### Communication

| Function      | Description             |
| ------------- | ----------------------- |
| `CHAT`        | Inter-node chat         |
| `PAGE`        | Page sysop/other users  |
| `WHOISONLINE` | Display who's connected |

#### Logoff

| Function          | Description                            |
| ----------------- | -------------------------------------- |
| `MAINLOGOFF`      | Logoff with confirmation + GOODBYE.ANS |
| `IMMEDIATELOGOFF` | Quit immediately without confirmation  |

#### News

| Function   | Description                                       |
| ---------- | ------------------------------------------------- |
| `LISTNEWS` | List/read all news items                          |
| `EDITNEWS` | SysOp news management (Add/Delete/Edit/List/View) |

#### Voting

| Function        | Description                             |
| --------------- | --------------------------------------- |
| `VOTE`          | Voting booths system                    |
| `VOTEMANDATORY` | Mandatory voting check (login sequence) |
| `LISTNUV`       | List NUV (New User Vote) candidates     |
| `SCANNUV`       | Vote on pending NUV candidates          |

#### BBS Directory

| Function        | Description                       |
| --------------- | --------------------------------- |
| `BBSLIST`       | List BBS directory entries        |
| `BBSLISTADD`    | Add new BBS listing               |
| `BBSLISTEDIT`   | Edit BBS listing (owner or sysop) |
| `BBSLISTDELETE` | Delete BBS listing                |
| `BBSLISTVERIFY` | SysOp: toggle verified flag       |

#### Rumors

| Function        | Description                           |
| --------------- | ------------------------------------- |
| `RUMORSLIST`    | List all rumors                       |
| `RUMORSADD`     | Add a new rumor                       |
| `RUMORSDELETE`  | Delete a rumor                        |
| `RUMORSSEARCH`  | Search rumors                         |
| `RUMORSNEWSCAN` | Rumors newscan (since last login)     |
| `RANDOMRUMOR`   | Display random rumor (login sequence) |

#### QWK Mail

| Function      | Description              |
| ------------- | ------------------------ |
| `QWKDOWNLOAD` | QWK mail packet download |
| `QWKUPLOAD`   | QWK REP packet upload    |

#### InfoForms

| Function           | Description                              |
| ------------------ | ---------------------------------------- |
| `INFOFORMS`        | InfoForms menu (list/fill/view forms)    |
| `INFOFORMVIEW`     | View own completed infoform              |
| `INFOFORMHUNT`     | SysOp: browse all users' completed forms |
| `INFOFORMREQUIRED` | Login sequence: force required forms     |
| `INFOFORMNUKE`     | SysOp: delete all forms for a user       |

#### Miscellaneous

| Function      | Description                                       |
| ------------- | ------------------------------------------------- |
| `ONELINER`    | One-liner system (quotes/comments)                |
| `PLACEHOLDER` | Handler for undefined/not-yet-implemented options |

### Door Types

The system supports four door execution types:

| Type          | Config Field            | Description                                |
| ------------- | ----------------------- | ------------------------------------------ |
| Native        | (default)               | Direct executable programs on any platform |
| DOS           | `IsDOS: true`           | Legacy DOS doors via dosemu2 emulator      |
| Synchronet JS | `Type: "synchronet_js"` | Synchronet-compatible JavaScript scripts   |
| Vision/3 VPL  | `Type: "v3_script"`     | Native Vision/3 JavaScript scripts         |

**Dropfile formats:** `DOOR.SYS`, `DOOR32.SYS`, `CHAIN.TXT`, `DORINFO1.DEF`, or none.

**I/O modes:** `STDIO` (default) or `SOCKET`.

---

## Scripting API — Vision/3 Native (V3)

Scripts are executed via the `v3_script` door type using the goja JavaScript engine. API objects are accessible under the `v3.*` namespace.

### v3.console — Terminal I/O

**Properties:**

| Property | Type                | Description             |
| -------- | ------------------- | ----------------------- |
| `width`  | integer (read-only) | Screen width in columns |
| `height` | integer (read-only) | Screen height in rows   |

**Output Methods:**

| Method              | Parameters         | Returns   | Description                         |
| ------------------- | ------------------ | --------- | ----------------------------------- |
| `write(text...)`    | string (variadic)  | undefined | Raw output, no pipe-code processing |
| `writeln(text...)`  | string (variadic)  | undefined | Raw output + CRLF                   |
| `print(text...)`    | string (variadic)  | undefined | Output with pipe-code processing    |
| `println(text...)`  | string (variadic)  | undefined | Print with pipe-codes + CRLF        |
| `clear()` / `cls()` | —                  | undefined | Clear screen, cursor to home        |
| `gotoxy(x, y)`      | integer, integer   | undefined | Position cursor (1-based)           |
| `color(fg[, bg])`   | integer[, integer] | undefined | Set foreground/background color     |
| `reset()`           | —                  | undefined | Reset terminal attributes           |
| `center(text)`      | string             | undefined | Center text on screen + CRLF        |

**Input Methods:**

| Method                   | Parameters         | Returns   | Description                                 |
| ------------------------ | ------------------ | --------- | ------------------------------------------- |
| `getkey([timeout_ms])`   | integer (optional) | string    | Read single key; `""` on timeout/disconnect |
| `getstr(maxlen[, opts])` | integer, object    | string    | Line input; opts: `{echo, upper, number}`   |
| `getnum([max])`          | integer (optional) | integer   | Read number, clamps to max                  |
| `yesno(prompt)`          | string             | boolean   | Y/n prompt (default Yes)                    |
| `noyes(prompt)`          | string             | boolean   | N/y prompt (default No)                     |
| `pause()`                | —                  | undefined | "[Press any key]" and wait                  |

### v3.session — Session Information

| Property            | Type              | Description                    |
| ------------------- | ----------------- | ------------------------------ |
| `node`              | integer           | Node number                    |
| `startTime`         | integer           | Session start (Unix timestamp) |
| `timeLeft`          | integer (dynamic) | Seconds remaining              |
| `online`            | boolean (dynamic) | User still connected           |
| `bbs.name`          | string            | BBS system name                |
| `bbs.sysop`         | string            | Sysop name                     |
| `bbs.version`       | string            | BBS version                    |
| `user.id`           | integer           | Current user's ID              |
| `user.handle`       | string            | Current user's handle          |
| `user.realName`     | string            | Current user's real name       |
| `user.accessLevel`  | integer           | User's access level            |
| `user.timesCalled`  | integer           | Times called                   |
| `user.location`     | string            | User's location                |
| `user.screenWidth`  | integer           | Screen width setting           |
| `user.screenHeight` | integer           | Screen height setting          |

### v3.user — Current User (Read/Write)

**Read-only Properties:** `id`, `handle`, `realName`, `accessLevel`, `timesCalled`, `location`, `messagesPosted`, `uploads`, `downloads`, `filePoints`, `validated`

| Method              | Parameters  | Returns   | Description                                                                   |
| ------------------- | ----------- | --------- | ----------------------------------------------------------------------------- |
| `set(field, value)` | string, any | undefined | Update writable field (`realName`, `location`, `screenWidth`, `screenHeight`) |
| `save()`            | —           | undefined | Persist changes to disk                                                       |

### v3.users — User Database (Read-Only)

| Method        | Parameters | Returns        | Description                           |
| ------------- | ---------- | -------------- | ------------------------------------- |
| `get(handle)` | string     | object \| null | Look up user by handle                |
| `getByID(id)` | integer    | object \| null | Look up user by ID                    |
| `count()`     | —          | integer        | Total registered user count           |
| `list()`      | —          | array          | Array of all users (safe fields only) |

### v3.message — Message Area Access

| Method                      | Parameters       | Returns        | Description                                         |
| --------------------------- | ---------------- | -------------- | --------------------------------------------------- |
| `areas()`                   | —                | array          | All message areas                                   |
| `area(tag)`                 | string           | object \| null | Get area by tag                                     |
| `count(areaID)`             | integer          | integer        | Message count in area                               |
| `get(areaID, msgNum)`       | integer, integer | object \| null | Get message by area and number                      |
| `newCount(areaID)`          | integer          | integer        | Unread message count for current user               |
| `post(areaID, opts)`        | integer, object  | integer        | Post message; opts: `{to, subject, body, replyTo?}` |
| `postPrivate(areaID, opts)` | integer, object  | integer        | Post private message                                |
| `totalCount()`              | —                | integer        | Total messages across all areas                     |

### v3.file — File Area Access

| Method          | Parameters | Returns        | Description                     |
| --------------- | ---------- | -------------- | ------------------------------- |
| `areas()`       | —          | array          | All file areas                  |
| `area(tag)`     | string     | object \| null | Get area by tag                 |
| `list(areaID)`  | integer    | array          | Files in the area               |
| `count(areaID)` | integer    | integer        | File count in area              |
| `search(query)` | string     | array          | Keyword search across all areas |
| `totalCount()`  | —          | integer        | Total files across all areas    |

### v3.fs — Sandboxed File Operations

All paths resolve relative to `scripts/data/`. Path traversal is blocked.

| Method                  | Parameters        | Returns   | Description                                     |
| ----------------------- | ----------------- | --------- | ----------------------------------------------- |
| `read(path)`            | string            | string    | Read text file                                  |
| `write(path, content)`  | string, string    | undefined | Write text file (overwrite)                     |
| `append(path, content)` | string, string    | undefined | Append to file                                  |
| `exists(path)`          | string            | boolean   | File/directory exists                           |
| `delete(path)`          | string            | boolean   | Delete file                                     |
| `list([dir])`           | string (optional) | array     | List directory; returns `[{name, isDir, size}]` |
| `mkdir(path)`           | string            | undefined | Create directory with parents                   |

### v3.data — Script-Local Persistent Storage

Each script gets its own JSON file in `scripts/data/<script-name>.json`.

| Method            | Parameters  | Returns          | Description                   |
| ----------------- | ----------- | ---------------- | ----------------------------- |
| `get(key)`        | string      | any \| undefined | Read value                    |
| `set(key, value)` | string, any | undefined        | Write JSON-serializable value |
| `delete(key)`     | string      | undefined        | Remove key                    |
| `keys()`          | —           | array            | All keys                      |
| `getAll()`        | —           | object           | Entire store as object        |

### v3.ansi — ANSI Art Display

Resolves files from: script directory → `menus/v3/ansi/` → `menus/v3/templates/`.

| Method                 | Parameters | Returns   | Description                                 |
| ---------------------- | ---------- | --------- | ------------------------------------------- |
| `display(filename)`    | string     | undefined | Display .ANS file with pipe-code processing |
| `displayRaw(filename)` | string     | undefined | Display .ANS file raw (CP437 bytes)         |

### v3.util — Utility Functions

| Method                         | Parameters                | Returns   | Description                                                 |
| ------------------------------ | ------------------------- | --------- | ----------------------------------------------------------- |
| `sleep(ms)`                    | integer                   | undefined | Pause execution                                             |
| `random(max)`                  | integer                   | integer   | Random 0 to max-1                                           |
| `time()`                       | —                         | integer   | Unix timestamp (seconds)                                    |
| `date([format])`               | string (optional)         | string    | Formatted date (Go format, default `"2006-01-02 15:04:05"`) |
| `padRight(str, width[, char])` | string, integer[, string] | string    | Right-pad string                                            |
| `padLeft(str, width[, char])`  | string, integer[, string] | string    | Left-pad string                                             |
| `center(str, width)`           | string, integer           | string    | Center string within width                                  |
| `stripAnsi(str)`               | string                    | string    | Remove ANSI escapes                                         |
| `stripPipe(str)`               | string                    | string    | Remove pipe codes                                           |
| `displayLen(str)`              | string                    | integer   | Visible length (ignores ANSI/pipe)                          |

### v3.nodes — Inter-Node Access

Invisible sessions are hidden from non-sysop users (accessLevel < 200).

| Method                   | Parameters      | Returns   | Description                                                         |
| ------------------------ | --------------- | --------- | ------------------------------------------------------------------- |
| `list()`                 | —               | array     | Active nodes; returns `[{node, handle, activity, idle, invisible}]` |
| `count()`                | —               | integer   | Active node count                                                   |
| `send(nodeNum, message)` | integer, string | boolean   | Page specific node                                                  |
| `broadcast(message)`     | string          | undefined | Page all nodes except self                                          |

### Top-Level Globals

| Name           | Type     | Description                                 |
| -------------- | -------- | ------------------------------------------- |
| `exit([code])` | function | Exit script with optional exit code         |
| `sleep(ms)`    | function | Alias for `v3.util.sleep()`                 |
| `v3.args`      | array    | Command-line arguments passed to the script |

---

## Scripting API — Synchronet Compatible (SyncJS)

Scripts are executed via the `synchronet_js` door type. Implements the Synchronet BBS global API for legacy door/module compatibility.

### console — Terminal Object

**Properties:**

| Property           | Type                 | Description                               |
| ------------------ | -------------------- | ----------------------------------------- |
| `screen_columns`   | integer (read-only)  | Terminal width                            |
| `screen_rows`      | integer (read-only)  | Terminal height                           |
| `line_counter`     | integer (read/write) | Line counter for pagination               |
| `attributes`       | integer (read/write) | Current text attribute byte               |
| `ctrlkey_passthru` | integer (read/write) | Ctrl key passthrough bitfield             |
| `autoterm`         | integer (read/write) | Terminal flags (USER_ANSI=1, USER_UTF8=4) |

**Output Methods:**

| Method             | Parameters        | Returns   | Description                    |
| ------------------ | ----------------- | --------- | ------------------------------ |
| `write(...args)`   | string (variadic) | undefined | Raw text output                |
| `writeln(...args)` | string (variadic) | undefined | Write with CRLF                |
| `print(...args)`   | string (variadic) | undefined | Write with Ctrl-A code parsing |
| `clear()`          | —                 | undefined | Clear screen                   |
| `home()`           | —                 | undefined | Cursor to top-left             |
| `cleartoeol()`     | —                 | undefined | Clear to end of line           |
| `gotoxy(x, y)`     | integer, integer  | undefined | Move cursor (1-indexed)        |
| `center(...args)`  | string (variadic) | undefined | Center text + CRLF             |
| `strlen(...args)`  | string (variadic) | integer   | Visible display length         |
| `right([count])`   | integer           | undefined | Move cursor right              |
| `left([count])`    | integer           | undefined | Move cursor left               |
| `up([count])`      | integer           | undefined | Move cursor up                 |
| `down([count])`    | integer           | undefined | Move cursor down               |

**Input Methods:**

| Method                     | Parameters       | Returns   | Description                  |
| -------------------------- | ---------------- | --------- | ---------------------------- |
| `getkey()`                 | —                | string    | Read single key (blocking)   |
| `inkey([timeout_ms])`      | integer          | string    | Read key with timeout        |
| `getstr([maxlen[, mode]])` | integer, integer | string    | Line input with mode flags   |
| `getkeys([valid_keys])`    | string           | string    | Read until valid key pressed |
| `getnum([max])`            | integer          | integer   | Read numeric input           |
| `pause()`                  | —                | undefined | "[Hit a key]" prompt         |
| `yesno(prompt)`            | string           | boolean   | Y/n prompt                   |
| `noyes(prompt)`            | string           | boolean   | N/y prompt                   |

### bbs — BBS Object

| Property/Method   | Type                 | Description                                                    |
| ----------------- | -------------------- | -------------------------------------------------------------- |
| `node_num`        | integer (read-only)  | Current node number                                            |
| `sys_status`      | integer (read/write) | System status flags                                            |
| `online`          | boolean (read-only)  | User still connected                                           |
| `logon_time`      | integer (read-only)  | Session start (Unix timestamp)                                 |
| `mods`            | object (read/write)  | Shared inter-module data                                       |
| `get_time_left()` | function → integer   | Seconds remaining                                              |
| `atcode(code)`    | function → string    | Resolve @-code (USER, ALIAS, NAME, REALNAME, NODE, SYSOP, BBS) |

### user — User Object

**Properties:** `alias`, `name`, `number`, `handle`, `full_name`, `level`, `location`, `settings`, `laston_date`, `expiration_date`, `new_file_time`, `birthdate`, `phone`, `comment`, `download_protocol`

**Nested Objects:**

| Path                          | Type    | Description                  |
| ----------------------------- | ------- | ---------------------------- |
| `user.security.level`         | integer | Access level                 |
| `user.security.password`      | string  | Always empty (never exposed) |
| `user.stats.total_logons`     | integer | Total login count            |
| `user.stats.total_posts`      | integer | Total message posts          |
| `user.stats.total_emails`     | integer | Total emails                 |
| `user.stats.files_uploaded`   | integer | Upload count                 |
| `user.stats.files_downloaded` | integer | Download count               |
| `user.stats.bytes_uploaded`   | integer | Bytes uploaded               |
| `user.stats.bytes_downloaded` | integer | Bytes downloaded             |

### system — System Object

| Property   | Type              | Description                       |
| ---------- | ----------------- | --------------------------------- |
| `name`     | string            | BBS system name                   |
| `operator` | string            | System operator                   |
| `qwk_id`   | string            | QWK ID (derived from name)        |
| `timer`    | integer (dynamic) | Current Unix time in milliseconds |
| `exec_dir` | string            | Executables directory             |
| `data_dir` | string            | Data directory                    |
| `node_dir` | string            | Node-specific directory           |
| `ctrl_dir` | string            | Control/config directory          |
| `text_dir` | string            | Text/resource directory           |
| `nodes`    | integer           | Number of nodes                   |

### File — File Class

```javascript
var file = new File(path)
```

**Properties:** `name`, `is_open`, `exists`, `position` (read/write), `length`, `error`, `eof`

**Methods:**

| Method                       | Parameters       | Returns        | Description                                          |
| ---------------------------- | ---------------- | -------------- | ---------------------------------------------------- |
| `open([mode])`               | string           | boolean        | Open file (modes: r, r+, w, w+, a, a+, rb, wb, etc.) |
| `close()`                    | —                | undefined      | Close file                                           |
| `flush()`                    | —                | boolean        | Sync to disk                                         |
| `read([bytes])`              | integer          | string         | Read bytes (default: all)                            |
| `readln()`                   | —                | string \| null | Read single line                                     |
| `readAll()`                  | —                | string[]       | Read all lines as array                              |
| `readBin([size])`            | integer          | integer        | Read little-endian unsigned int (1/2/4 bytes)        |
| `write(data)`                | string           | boolean        | Write string                                         |
| `writeln(data)`              | string           | boolean        | Write string + LF                                    |
| `writeAll(lines)`            | string[]         | boolean        | Write array of lines                                 |
| `writeBin(value[, size])`    | integer, integer | boolean        | Write little-endian unsigned int                     |
| `truncate([length])`         | integer          | boolean        | Truncate file                                        |
| `lock([offset[, length]])`   | integer, integer | boolean        | Lock byte range                                      |
| `unlock([offset[, length]])` | integer, integer | boolean        | Unlock byte range                                    |

**INI Methods:**

| Method                                   | Parameters          | Returns             | Description            |
| ---------------------------------------- | ------------------- | ------------------- | ---------------------- |
| `iniGetValue([section,] key[, default])` | string, string, any | string \| undefined | Get INI key value      |
| `iniGetSections()`                       | —                   | string[]            | List all section names |
| `iniGetKeys([section])`                  | string              | string[]            | List keys in section   |

### Queue — Message Queue

```javascript
var queue = new Queue()
```

| Method/Property      | Parameters | Returns             | Description                 |
| -------------------- | ---------- | ------------------- | --------------------------- |
| `write(value)`       | any        | boolean             | Enqueue value               |
| `read()`             | —          | any                 | Dequeue value               |
| `poll([timeout_ms])` | integer    | boolean             | Check availability          |
| `peek()`             | —          | any                 | Front item without removing |
| `data_waiting`       | —          | boolean (read-only) | Items in queue              |

### js — Module System

| Property/Method     | Type                | Description                   |
| ------------------- | ------------------- | ----------------------------- |
| `exec_dir`          | string (read-only)  | Directory of current script   |
| `load_path_list`    | array (read/write)  | Library search paths          |
| `terminated`        | boolean (read-only) | Termination requested         |
| `global`            | object (read-only)  | Reference to global scope     |
| `on_exit(callback)` | function            | Register cleanup (LIFO order) |
| `gc()`              | function            | No-op (Go manages GC)         |

### server / client — Connection Stubs

| Path                       | Value                                   | Description         |
| -------------------------- | --------------------------------------- | ------------------- |
| `server.version`           | `"ViSiON/3 SyncJS"`                     | Server version      |
| `server.version_detail`    | `"ViSiON/3 SyncJS Compatibility Layer"` | Detailed version    |
| `client.protocol`          | `"SSH"`                                 | Connection protocol |
| `client.ip_address`        | `"127.0.0.1"`                           | Client IP           |
| `client.socket.descriptor` | `-1`                                    | Socket FD (stub)    |

### Global Functions

**Module Loading:**

| Function                                | Parameters             | Returns      | Description                                         |
| --------------------------------------- | ---------------------- | ------------ | --------------------------------------------------- |
| `load([background,] filename, ...args)` | boolean, string, any   | any \| Queue | Load/execute JS file; background=true returns Queue |
| `require([scope,] filename[, symbol])`  | object, string, string | undefined    | Load and extract symbol into scope                  |
| `exit([code])`                          | integer                | undefined    | Terminate script                                    |

**Time:**

| Function                        | Parameters      | Returns   | Description              |
| ------------------------------- | --------------- | --------- | ------------------------ |
| `time()`                        | —               | integer   | Unix timestamp (seconds) |
| `sleep(ms)` / `mswait(ms)`      | integer         | undefined | Sleep milliseconds       |
| `strftime(format[, timestamp])` | string, integer | string    | C-style time format      |

**String:**

| Function               | Parameters        | Returns           | Description               |
| ---------------------- | ----------------- | ----------------- | ------------------------- |
| `format(fmt, ...args)` | string, any       | string            | Printf-style formatting   |
| `ascii(char_or_code)`  | string \| integer | integer \| string | ASCII conversion          |
| `ascii_str(code)`      | integer           | string            | ASCII code to character   |
| `truncsp(str)`         | string            | string            | Trim trailing whitespace  |
| `backslash(path)`      | string            | string            | Ensure trailing separator |

**Math & Random:**

| Function        | Parameters | Returns | Description                     |
| --------------- | ---------- | ------- | ------------------------------- |
| `random([max])` | integer    | integer | Random 0 to max-1 (default 100) |

**File Operations:**

| Function                       | Parameters     | Returns             | Description                 |
| ------------------------------ | -------------- | ------------------- | --------------------------- |
| `file_exists(path)`            | string         | boolean             | Check file exists           |
| `file_remove(path)`            | string         | boolean             | Delete file                 |
| `file_isdir(path)`             | string         | boolean             | Check if directory          |
| `file_size(path)`              | string         | integer             | File size (-1 if not found) |
| `file_getcase(path)`           | string         | string \| undefined | Case-insensitive lookup     |
| `file_rename(old, new)`        | string, string | boolean             | Rename/move file            |
| `file_mutex(filename[, text])` | string, string | boolean             | Atomic lock file            |
| `directory(pattern)`           | string         | string[]            | Glob pattern file listing   |
| `mkdir(path)`                  | string         | boolean             | Create directory            |

**Logging:**

| Function                | Parameters      | Returns   | Description                 |
| ----------------------- | --------------- | --------- | --------------------------- |
| `log([level,] message)` | integer, string | undefined | Log message (syslog levels) |
| `alert(message)`        | string          | undefined | Log as INFO                 |

**Constants:**

| Name                    | Value |
| ----------------------- | ----- |
| `LOG_EMERG`             | 0     |
| `LOG_ALERT`             | 1     |
| `LOG_CRIT`              | 2     |
| `LOG_ERR` / `LOG_ERROR` | 3     |
| `LOG_WARNING`           | 4     |
| `LOG_NOTICE`            | 5     |
| `LOG_INFO`              | 6     |
| `LOG_DEBUG`             | 7     |

**Polyfills:** `Object.prototype.toSource()`, `Array.prototype.toSource()`, `Date.prototype.getYear()`, bare function expression support in `eval()`.

---

## Error Handling

Most functions return an error as the last return value:

```go
result, err := someFunction()
if err != nil {
    // Handle error
}
```

Error wrapping convention: `fmt.Errorf("context: %w", err)`. Use `errors.Is` / `errors.As` for inspection.

## Logging

The system uses Go's `slog` (Go 1.21+) for structured logging. Log entries include:

- Node number
- User information (when available)
- Severity level (INFO, WARN, ERROR, DEBUG)

## Session Context

Sessions carry context including:

- SSH/Telnet session reference
- Terminal instance
- User reference (after authentication)
- Node ID
- Start time
- Auto-run tracker
- Transfer state (for ZMODEM/YMODEM/XMODEM)
- Read interrupt channel (for door I/O cancellation)
