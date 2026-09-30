package editor

import (
	"io"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/gliderlabs/ssh"
)

// The full-screen message editor: construction, the settings a caller supplies
// before it runs, and the Run loop itself. Key dispatch, text edits and cursor
// movement live in the fseditor_* files beside this one.

// FSEditor is the full-screen message editor
type FSEditor struct {
	// Components
	buffer      *MessageBuffer
	screen      *Screen
	input       *InputHandler
	ownsInput   bool // input was created by NewFSEditor, so Run must close it on exit
	wordWrapper *WordWrapper
	commands    *CommandHandler

	// Cursor position (1-based)
	currentLine int
	currentCol  int

	// View state
	topLine     int // First line visible on screen
	lastTopLine int // Track last topLine to detect scrolling

	// Editor state
	insertMode     bool
	lastInsertMode bool
	modified       bool
	saved          bool
	quit           bool

	// Metadata
	subject     string
	recipient   string
	fromName    string // sender display name: handle, real name, or anonymous string
	isAnon      bool
	menuSetPath string

	// Terminal
	session    ssh.Session
	outputMode ansi.OutputMode
	resizes    <-chan ssh.Window // window changes for Run to apply between keys (nil = none)
}

// NewFSEditor creates a new full-screen editor instance.
// ih is an optional pre-created InputHandler to reuse (pass nil to create a new one).
// Passing a shared InputHandler prevents the editor's background goroutine from
// racing with the caller's reader for bytes after the editor exits.
func NewFSEditor(session ssh.Session, terminal io.Writer, outputMode ansi.OutputMode,
	termWidth, termHeight int, menuSetPath, yesNoHi, yesNoLo, yesText, noText, abortText string,
	ih *InputHandler) *FSEditor {

	buffer := NewMessageBuffer()
	screen := NewScreen(terminal, outputMode, termWidth, termHeight)
	ownsInput := false
	if ih == nil {
		ih = NewInputHandler(session)
		ownsInput = true
	}
	input := ih
	wordWrapper := NewWordWrapper(buffer)
	commandHandler := NewCommandHandler(screen, buffer, menuSetPath, yesNoHi, yesNoLo, yesText, noText, abortText)

	return &FSEditor{
		buffer:         buffer,
		screen:         screen,
		input:          input,
		ownsInput:      ownsInput,
		wordWrapper:    wordWrapper,
		commands:       commandHandler,
		currentLine:    1,
		currentCol:     1,
		topLine:        1,
		lastTopLine:    1,
		insertMode:     true,
		lastInsertMode: true,
		modified:       false,
		saved:          false,
		quit:           false,
		session:        session,
		outputMode:     outputMode,
		menuSetPath:    menuSetPath,
	}
}

// SetMetadata sets the message metadata (subject, recipient, sender, etc.)
func (e *FSEditor) SetMetadata(subject, recipient, fromName string, isAnon bool) {
	e.subject = subject
	e.recipient = recipient
	e.fromName = fromName
	e.isAnon = isAnon
}

// SetTimezone configures the timezone used for date/time display in the editor header.
func (e *FSEditor) SetTimezone(configTZ string) {
	e.screen.configTimezone = configTZ
}

// SetBoardName sets the BBS board name substituted into the footer @B@ placeholder.
func (e *FSEditor) SetBoardName(name string) {
	e.screen.boardName = name
}

// SetEditorContext sets optional context fields displayed in the editor header
// (node number, next message number, conference > area name).
func (e *FSEditor) SetEditorContext(ctx EditorContext) {
	e.screen.nodeNumber = ctx.NodeNumber
	e.screen.nextMsgNum = ctx.NextMsgNum
	e.screen.confArea = ctx.ConfArea
}

// SetQuoteData sets message data to be used for the /Q quote command
func (e *FSEditor) SetQuoteData(data *QuoteData) {
	e.commands.SetQuoteData(data)
}

// SetQuoteStrings overrides the quote block styling (banners and per-line
// prefix) with the values configured in strings.json.
func (e *FSEditor) SetQuoteStrings(top, bottom, prefix string) {
	e.commands.SetQuoteStrings(top, bottom, prefix)
}

// LoadContent loads initial content into the editor
func (e *FSEditor) LoadContent(content string) {
	e.buffer.LoadContent(content)
	if content != "" {
		e.modified = true
		// Position at end of content
		lineCount := e.buffer.GetLineCount()
		e.currentLine = lineCount
		e.currentCol = e.buffer.GetLineLength(lineCount) + 1
	}
}

// Run starts the editor main loop
func (e *FSEditor) Run() (string, bool, error) {
	// A self-created InputHandler must not outlive the editor: its goroutine
	// would keep reading the session and steal alternate keystrokes from the
	// caller's reader (the "double key press" bug). Shared handlers stay open —
	// they belong to the session, not the editor.
	if e.ownsInput {
		defer e.input.CloseAndWait()
	}
	// Load and display header (non-fatal on error - continue with minimal header)
	_ = e.screen.LoadHeaderTemplate(e.menuSetPath, e.subject, e.recipient, e.fromName, e.isAnon)

	// Load footer template — adjusts statusLineY to reserve the bottom 2 rows.
	// Must be called after LoadHeaderTemplate so editingStartY is already set from |#N.
	_ = e.screen.LoadFooterTemplate(e.menuSetPath)

	// Initial screen draw
	e.screen.FullRedraw(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode)

	// Main edit loop
	for !e.quit {
		// Read a key, or a window change that arrives while waiting for one.
		// Resizes are applied here, on the session goroutine, because Screen
		// is not safe for concurrent use. One that arrives while a prompt or
		// the quote picker is reading keys waits until that returns.
		key, win, resized, err := readKeyOrEvent(e.input, e.resizes)
		if err != nil {
			return "", false, err
		}
		if resized {
			e.HandleResize(win.Width, win.Height)
			continue
		}

		// Handle the key
		e.handleKey(key)

		// Ensure view is updated to keep cursor visible
		e.ensureCursorVisible()

		// Determine if we need to update status line fields
		statusChanged := (e.insertMode != e.lastInsertMode) || (e.topLine != e.lastTopLine)

		// Refresh screen
		e.screen.RefreshScreen(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode, statusChanged)

		// Update tracking variables
		e.lastInsertMode = e.insertMode
		e.lastTopLine = e.topLine
	}

	// Return final content and saved status
	content := e.buffer.GetContent()
	return content, e.saved, nil
}

// redrawScreen forces a complete screen redraw
func (e *FSEditor) redrawScreen() {
	e.screen.FullRedraw(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode)
}

// HandleResize applies a terminal size change and redraws the screen. Sizes
// below 80x24 are raised to it, as at startup. A size equal to the current
// one is ignored, so the initial window report that an SSH session queues
// does not cause a second full redraw.
//
// HandleResize touches the Screen, so it must only be called from the
// goroutine running Run, or while Run is not running. RunEditorWithMetadata
// hands Run the session's window changes so Run calls it between keys.
func (e *FSEditor) HandleResize(newWidth, newHeight int) {
	newWidth, newHeight = clampTermSize(newWidth, newHeight)
	if newWidth == e.screen.termWidth && newHeight == e.screen.termHeight {
		return
	}
	e.screen.Resize(newWidth, newHeight)
	e.ensureCursorVisible() // a shorter window may have pushed the cursor off the bottom
	e.screen.FullRedraw(e.buffer, e.topLine, e.currentLine, e.currentCol, e.insertMode)
}

// clampTermSize raises a terminal size to the 80x24 the editor needs.
func clampTermSize(width, height int) (int, int) {
	if width < 80 {
		width = 80
	}
	if height < 24 {
		height = 24
	}
	return width, height
}
