package menu

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
)

// sessionReadLine returns a ReadLineFunc that reads through the session's shared
// InputHandler, preventing stale bytes from being left for subsequent readers.
func sessionReadLine(s ssh.Session, terminal *term.Terminal) ziplab.ReadLineFunc {
	return func() (string, error) { return readLineFromSessionIH(s, terminal) }
}

// sessionReadKey returns a ReadKeyFunc that reads through the session's shared InputHandler.
func sessionReadKey(s ssh.Session) ziplab.ReadKeyFunc {
	return func() (int, error) { return getSessionIH(s).ReadKey() }
}

// zipLabSender returns the ZipLab callback that sends one archive member
// from record's area. Like the other download paths it re-checks the area's
// download ACS and lets the user pick a protocol — both before the member
// is extracted — then hands the session to the transfer program through
// runTransferSend, which stops the shared InputHandler first so it cannot
// steal the protocol's bytes.
func (e *MenuExecutor) zipLabSender(s ssh.Session, terminal *term.Terminal, record *file.FileRecord, u *user.User, userManager *user.UserMgr, nodeNumber int, sessionStartTime time.Time, outputMode ansi.OutputMode) ziplab.SendFunc {
	return func(extract ziplab.ExtractFunc) {
		area, ok := e.FileMgr.GetAreaByID(record.AreaID)
		if !ok || u == nil || (area.ACSDownload != "" && !checkACS(area.ACSDownload, u, s, terminal, sessionStartTime)) {
			e.writeString(terminal, outputMode, e.Strings().YouCantDownloadHere)
			return
		}
		proto, protoOK, protoErr := e.selectTransferProtocol(s, terminal, outputMode)
		if protoErr != nil {
			if !errors.Is(protoErr, io.EOF) {
				slog.Error("protocol selection error", "node", nodeNumber, "error", protoErr)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01"+protocolSelectionErrorText(protoErr)+"|07\r\n")), outputMode)
			}
			return
		}
		if !protoOK {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Download cancelled.|07\r\n")), outputMode)
			return
		}
		path, err := extract()
		if err != nil {
			return
		}
		success, _ := e.runTransferSend(s, terminal, proto, []string{path}, nil, outputMode, nodeNumber, u)
		if success > 0 {
			if err := userManager.UpdateUser(u); err != nil {
				slog.Error("saving archive-member download statistics", "node", nodeNumber, "error", err)
			}
		}
	}
}

// findFileInArea searches for a file by name (case-insensitive) in the given area.
func findFileInArea(fm *file.FileManager, areaID int, filename string) (*file.FileRecord, error) {
	files := fm.GetFilesForArea(areaID)
	lowerFilename := strings.ToLower(filename)

	for i := range files {
		if strings.ToLower(files[i].Filename) == lowerFilename {
			return &files[i], nil
		}
	}
	return nil, fmt.Errorf("file not found: %s", filename)
}

// promptAndResolveFile handles the shared logic for file viewing commands:
// validates the user/area, prompts for filename, looks up the record, and resolves the path.
func promptAndResolveFile(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, currentUser *user.User, nodeNumber int, promptVerb string, outputMode ansi.OutputMode) (*file.FileRecord, string, *user.User, string, error) {
	if currentUser == nil {
		return nil, "", nil, "", nil
	}

	currentAreaID := currentUser.CurrentFileAreaID
	if currentAreaID <= 0 {
		e.writeString(terminal, outputMode, e.Strings().FileNoAreaSelected)
		uiPause(1 * time.Second)
		return nil, "", currentUser, "", nil
	}

	e.writeStringf(terminal, outputMode, e.Strings().FilePromptFormat, promptVerb)

	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "", nil, "LOGOFF", io.EOF
		}
		return nil, "", currentUser, "", fmt.Errorf("failed reading input: %w", err)
	}

	filename := strings.TrimSpace(input)
	if filename == "" {
		return nil, "", currentUser, "", nil
	}

	record, err := findFileInArea(e.FileMgr, currentAreaID, filename)
	if err != nil {
		e.writeStringf(terminal, outputMode, e.Strings().FileNotFoundFormat, filename)
		uiPause(1 * time.Second)
		return nil, "", currentUser, "", nil
	}

	filePath, err := e.FileMgr.GetFilePath(record.ID)
	if err != nil {
		slog.Error("failed to get path for file", "node", nodeNumber, "id", record.ID, "error", err)
		e.writeString(terminal, outputMode, e.Strings().FileLocateError)
		uiPause(1 * time.Second)
		return nil, "", currentUser, "", nil
	}

	return record, filePath, currentUser, "", nil
}

// runViewFile prompts for a filename and displays it intelligently:
// archives show their contents listing, text files are displayed with paging.
func runViewFile(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termHeight := c.termHeight

	slog.Debug("running VIEW_FILE", "node", nodeNumber)

	record, _, retUser, retAction, retErr := promptAndResolveFile(e, s, terminal, currentUser, nodeNumber, "view", outputMode)
	if record == nil {
		return retUser, retAction, retErr
	}

	send := e.zipLabSender(s, terminal, record, currentUser, c.userManager, nodeNumber, c.sessionStartTime, outputMode)
	viewFileByRecord(e, viewerCaller{s: s, terminal: terminal, user: currentUser, userManager: c.userManager,
		nodeNumber: nodeNumber, sessionStartTime: c.sessionStartTime, outputMode: outputMode,
		width: c.termWidth, height: termHeight}, record, send)
	return currentUser, "", nil
}

// runTypeTextFile prompts for a filename and displays it as raw text with paging.
func runTypeTextFile(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termHeight := c.termHeight

	slog.Debug("running TYPE_TEXT_FILE", "node", nodeNumber)

	record, filePath, retUser, retAction, retErr := promptAndResolveFile(e, s, terminal, currentUser, nodeNumber, "type", outputMode)
	if record == nil {
		return retUser, retAction, retErr
	}

	// The text viewer door, when one is set, shows it; otherwise the pager.
	vc := viewerCaller{s: s, terminal: terminal, user: currentUser, userManager: c.userManager,
		nodeNumber: nodeNumber, sessionStartTime: c.sessionStartTime, outputMode: outputMode,
		width: c.termWidth, height: termHeight}
	if e.viewWithDoor(vc, record, e.GetServerConfig().TextViewerDoor) {
		return currentUser, "", nil
	}
	if termHeight <= 0 {
		_, termHeight = getTerminalSize(s)
	}
	e.displayTextWithPaging(s, terminal, currentUser, filePath, record.Filename, outputMode, termHeight)

	return currentUser, "", nil
}

// viewerCaller is who a file is being viewed for and on what terminal, as
// the viewers need it.
type viewerCaller struct {
	s                ssh.Session
	terminal         *term.Terminal
	user             *user.User
	userManager      *user.UserMgr
	nodeNumber       int
	sessionStartTime time.Time
	outputMode       ansi.OutputMode
	width, height    int
}

// viewFileByRecord displays a file given its record, from either file list
// or VIEW_FILE: in the viewer door config.json names for its kind
// (archiveViewerDoor or textViewerDoor) when there is one the caller may
// use, otherwise built in, archives in ZipLab (extracting members through
// send) and everything else in the text pager.
func viewFileByRecord(e *MenuExecutor, vc viewerCaller, record *file.FileRecord, send ziplab.SendFunc) {
	filePath, err := e.FileMgr.GetFilePath(record.ID)
	if err != nil {
		slog.Error("failed to get path for file", "id", record.ID, "error", err)
		e.writeString(vc.terminal, vc.outputMode, e.Strings().FileLocateError)
		uiPause(1 * time.Second)
		return
	}

	isArchive := e.FileMgr.IsSupportedArchive(filePath)
	door := e.GetServerConfig().TextViewerDoor
	if isArchive {
		door = e.GetServerConfig().ArchiveViewerDoor
	}
	if e.viewWithDoor(vc, record, door) {
		return
	}

	if isArchive {
		ziplab.RunZipLabView(vc.terminal, filePath, record.Filename, vc.outputMode, sessionReadLine(vc.s, vc.terminal), sessionReadKey(vc.s), send)
		return
	}
	height := vc.height
	if height <= 0 {
		_, height = getTerminalSize(vc.s)
	}
	e.displayTextWithPaging(vc.s, vc.terminal, vc.user, filePath, record.Filename, vc.outputMode, height)
}

// viewerDoor looks up the viewer door named code: ok when code is set and
// names a v3_script door. why says what's wrong otherwise, "" for an unset
// code.
func (e *MenuExecutor) viewerDoor(code string) (door config.DoorConfig, why string, ok bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return config.DoorConfig{}, "", false
	}
	door, found := e.GetDoorConfig(code)
	switch {
	case !found:
		return door, "no door has that code", false
	case door.Type != "v3_script":
		return door, "it is not a v3_script door", false
	}
	return door, "", true
}

// viewWithDoor runs the viewer door named code for record, with the area tag
// and filename after the door's own arguments, and reports whether it ran.
// It doesn't run, and the caller uses the built-in viewer, when code is
// empty, names no v3_script door (WarnViewerDoors reports that at startup),
// or the door's own rules (min_access_level and acs) refuse the caller. It
// also reports false, so the built-in viewer shows the file after all, when
// the door is a single_instance door in use on another node, or fails (the
// caller is shown the door error first, as the door menu shows it).
func (e *MenuExecutor) viewWithDoor(vc viewerCaller, record *file.FileRecord, code string) bool {
	if vc.user == nil {
		return false
	}
	door, _, ok := e.viewerDoor(code)
	if !ok {
		return false
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if !doorAllowed(door, vc.user, vc.s, vc.terminal, vc.sessionStartTime) {
		slog.Debug("viewer door refused the caller; using the built-in viewer", "node", vc.nodeNumber, "door", code)
		return false
	}
	area, ok := e.FileMgr.GetAreaByID(record.AreaID)
	if !ok {
		return false
	}
	door.Args = append(append([]string(nil), door.Args...), area.Tag, record.Filename)

	ctx := buildDoorCtx(e, vc.s, vc.terminal,
		vc.user.ID, vc.user.Handle, vc.user.RealName,
		vc.user.AccessLevel, vc.user.TimeLimit, vc.user.TimesCalled,
		vc.user.GroupLocation,
		vc.width, vc.height,
		vc.nodeNumber, vc.sessionStartTime, vc.outputMode,
		door, code)
	// The live record, so a file the viewer marks is marked in the list too.
	ctx.UserManager = vc.userManager
	ctx.CurrentUser = vc.user

	handOffSessionIH(vc.s)
	err := executeDoor(ctx)
	_ = getSessionIH(vc.s)
	switch {
	case err == nil, isSessionFatal(err):
		// A session-ending error is seen again by the caller's next read.
		return true
	case errors.Is(err, ErrDoorBusy):
		// Another node has a single_instance viewer open.
		slog.Info("viewer door busy; using the built-in viewer", "node", vc.nodeNumber, "door", code)
		return false
	default:
		slog.Error("viewer door failed", "node", vc.nodeNumber, "door", code, "file", record.Filename, "error", err)
		if mErr := doorErrorMessage(ctx, fmt.Sprintf("Error running viewer '%s': %v", code, err)); isSessionFatal(mErr) {
			return true
		}
		return false
	}
}

// WarnViewerDoors logs a warning for each viewer door config.json names
// that the BBS can't use (missing, or not a v3_script door), so a sysop
// learns why the built-in viewer runs instead. Called once at startup.
func (e *MenuExecutor) WarnViewerDoors() {
	cfg := e.GetServerConfig()
	for _, v := range []struct{ key, code string }{
		{"archiveViewerDoor", cfg.ArchiveViewerDoor},
		{"textViewerDoor", cfg.TextViewerDoor},
	} {
		if _, why, ok := e.viewerDoor(v.code); !ok && why != "" {
			slog.Warn("viewer door can't be used; the built-in viewer runs instead", "setting", v.key, "door", v.code, "problem", why)
		}
	}
}

// maxTextFilePagingBytes caps how large a file can be before displayTextWithPaging
// refuses to load it entirely into memory.  BBS text files (NFO, docs, source)
// are almost always well under this; the cap guards against accidental OOM when
// a user points the viewer at a large binary or log file.
const maxTextFilePagingBytes = 4 * 1024 * 1024 // 4 MB

// displayTextWithPaging shows text file contents to u, a screen at a time
// unless u has turned More prompts off (see morePager).
func (e *MenuExecutor) displayTextWithPaging(s ssh.Session, terminal *term.Terminal, u *user.User, filePath string, filename string, outputMode ansi.OutputMode, termHeight int) {
	fi, statErr := os.Stat(filePath)
	if statErr == nil && fi.Size() > maxTextFilePagingBytes {
		slog.Warn("file too large for text paging, refusing to load", "file", filePath, "bytes", fi.Size())
		e.writeString(terminal, outputMode, e.Strings().FileOpenError)
		uiPause(1 * time.Second)
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		slog.Error("failed to open file", "file", filePath, "error", err)
		e.writeString(terminal, outputMode, e.Strings().FileOpenError)
		uiPause(1 * time.Second)
		return
	}

	// Strip SAUCE metadata before displaying so binary SAUCE bytes don't
	// appear as garbled characters or "?" in the output.
	data = stripSauceMetadata(data)

	// For UTF-8 terminals, convert CP437 high bytes to their Unicode
	// equivalents one byte at a time.  writeUTF8Mode's heuristic of
	// "keep valid UTF-8 sequences" incorrectly passes CP437 byte pairs
	// that happen to form valid 2-byte UTF-8 sequences (e.g. 0xDB 0xB2 →
	// U+06F2) through untouched, and WriteStringCP437 has the same problem
	// in reverse (can't find U+06F2 in UnicodeToCP437 → outputs '?').
	// Pre-converting here means file content is written directly to the
	// terminal without any further encoding conversion.
	if outputMode == ansi.OutputModeUTF8 {
		data = ansi.ConvertCP437ToUTF8(data)
	}
	// For CP437 terminals the bytes are already correct as-is.

	// Clear screen before showing the file.
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	e.writeStringf(terminal, outputMode, e.Strings().FileViewingHeader, sanitizeControlChars(filename))

	// The header and the prompt take the other rows. The pager keeps the
	// text pager's own prompt, so a board's wording for it stays.
	pager := e.newMorePager(s, terminal, outputMode, u, termHeight, 4, e.Strings().FileMorePrompt)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 4096)

	var lineBuf []byte
	for scanner.Scan() {
		// Copy the line before adding CRLF: scanner.Bytes() is a view into the
		// scanner's buffer, so appending to it in place would overwrite the
		// start of the next line.
		lineBuf = append(lineBuf[:0], scanner.Bytes()...)
		lineBuf = append(lineBuf, '\r', '\n')
		// Write directly: data is already in the correct encoding
		// (UTF-8 after ConvertCP437ToUTF8, or raw CP437 for CP437 terminals).
		// Going through WriteProcessedBytes would re-interpret the bytes and
		// produce '?' for byte pairs that form false UTF-8 sequences.
		_, _ = terminal.Write(lineBuf) // best-effort display
		if !pager.Line() {
			return
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("error reading file", "file", filePath, "error", err)
	}

	e.writeString(terminal, outputMode, e.Strings().FileEndOfFile)
	e.pauseEnter(s, terminal, outputMode, e.Strings().FilePausePrompt)
}

// pauseEnter displays a simple "press Enter" prompt.
func (e *MenuExecutor) pauseEnter(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, prompt string) {
	e.writeString(terminal, outputMode, prompt)

	ih := getSessionIH(s)
	for {
		key, err := ih.ReadKey()
		if err != nil {
			return
		}
		if key == int('\r') || key == int('\n') {
			return
		}
	}
}

// getTerminalSize returns the terminal width and height from the session.
func getTerminalSize(s ssh.Session) (int, int) {
	ptyReq, _, isPty := s.Pty()
	if isPty && ptyReq.Window.Width > 0 && ptyReq.Window.Height > 0 {
		return ptyReq.Window.Width, ptyReq.Window.Height
	}
	return 80, 24
}

// displayTextWithPaging_toWriter writes text file contents to a writer (testable).
func displayTextWithPaging_toWriter(w io.Writer, filePath string, filename string, termHeight int) {
	f, err := os.Open(filePath)
	if err != nil {
		slog.Error("failed to open file", "file", filePath, "error", err)
		_, _ = fmt.Fprintf(w, "\r\nError opening file.\r\n") // best-effort display
		return
	}
	defer func() { _ = f.Close() }() // read-only

	_, _ = fmt.Fprintf(w, "\r\n--- Viewing: %s ---\r\n\r\n", sanitizeControlChars(filename)) // best-effort display

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 4096)

	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintf(w, "%s\r\n", line) // best-effort display
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("error reading file", "file", filePath, "error", err)
	}

	_, _ = fmt.Fprintf(w, "\r\n--- End of File ---\r\n") // best-effort display
}
