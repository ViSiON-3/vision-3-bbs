package editor

import (
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// Footer/lightbar color constants for consistent editor UI styling.
const (
	footerTextColor    = "\x1b[37m"      // White for regular footer text
	footerSpecialColor = "\x1b[1;34m"    // Bright blue for special chars/punctuation
	lbSelected         = "\x1b[1;36;44m" // Bright cyan on blue bg (selected lightbar item)
	lbUnselected       = "\x1b[37m"      // White (unselected lightbar item)
)

// CommandType represents a special editor command
type CommandType int

const (
	CommandNone  CommandType = iota
	CommandSave              // /S - Save and exit
	CommandAbort             // /A - Abort editing
	CommandQuote             // /Q - Quote previous message
	CommandHelp              // /H or /? - Show help
	CommandView              // /V - View message (not implemented in this version)
)

// QuoteData holds message metadata for quoting
type QuoteData struct {
	From   string   // Message author
	Title  string   // Message subject/title
	Date   string   // Message date
	Time   string   // Message time
	IsAnon bool     // Anonymous flag
	Lines  []string // Message content lines
}

// CommandHandler handles special slash commands
type CommandHandler struct {
	screen      *Screen
	buffer      *MessageBuffer
	quoteData   *QuoteData // Message data to quote when /Q is used
	menuSetPath string     // Path to menu files
	yesNoHi     string     // ANSI sequence for highlighted Yes/No
	yesNoLo     string     // ANSI sequence for regular Yes/No
	yesText     string     // Configurable Yes label
	noText      string     // Configurable No label
	abortText   string     // Configurable abort confirmation prompt

	// Quote block styling, from strings.json (empty = use the built-in default).
	quoteTopStr    string // banner above the quoted lines, supports ^N/^T/^D/^W
	quoteBottomStr string // banner below the quoted lines
	quotePrefixStr string // per-line prefix template, supports ^I (initials) and ^N
}

// Built-in quote block styling, used when strings.json supplies none.
const (
	defaultQuoteTop    = "|08--- |15^N |07Said |08---"
	defaultQuoteBottom = "|08--- |15^N |07Done |08---|07"
	defaultQuotePrefix = "^I> "
)

// NewCommandHandler creates a new command handler
func NewCommandHandler(screen *Screen, buffer *MessageBuffer, menuSetPath, yesNoHi, yesNoLo, yesText, noText, abortText string) *CommandHandler {
	yesText = strings.TrimSpace(yesText)
	if yesText == "" {
		yesText = "Yes"
	}

	noText = strings.TrimSpace(noText)
	if noText == "" {
		noText = "No"
	}

	abortText = strings.TrimSpace(abortText)
	if abortText == "" {
		abortText = "|14Abort message?"
	}

	return &CommandHandler{
		screen:      screen,
		buffer:      buffer,
		menuSetPath: menuSetPath,
		yesNoHi:     yesNoHi,
		yesNoLo:     yesNoLo,
		yesText:     yesText,
		noText:      noText,
		abortText:   abortText,
	}
}

// SetQuoteData sets the message data to be used for the /Q quote command
func (ch *CommandHandler) SetQuoteData(data *QuoteData) {
	ch.quoteData = data
}

// SetQuoteStrings overrides the quote block styling from strings.json.
// Empty values keep the built-in defaults.
func (ch *CommandHandler) SetQuoteStrings(top, bottom, prefix string) {
	ch.quoteTopStr = top
	ch.quoteBottomStr = bottom
	ch.quotePrefixStr = prefix
}

// HandleSave handles the Save command (CTRL-Z).
// Returns true to signal save and exit; on false, an error is written to PromptRow.
func (ch *CommandHandler) HandleSave() bool {
	content := ch.buffer.GetContent()
	if strings.TrimSpace(content) == "" {
		ch.screen.GoXY(1, ch.screen.PromptRow())
		ch.screen.ClearEOL()
		ch.screen.WriteDirectProcessed("|12Cannot save empty message! Press any key...")
		return false
	}
	return true
}

// HandleAbort handles the Abort command (CTRL-A).
// Displays a lightbar Yes/No confirmation in the last footer row (PromptRow).
// Returns true to signal abort and exit; false restores the footer row and continues.
func (ch *CommandHandler) HandleAbort(inputHandler *InputHandler) bool {
	promptRow := ch.screen.PromptRow()

	// Without a footer, clear the row above the prompt as a visual separator.
	// With a footer the row above is the first footer row — leave it intact.
	if !ch.screen.HasFooter() && promptRow > 1 {
		ch.screen.GoXY(1, promptRow-1)
		ch.screen.ClearEOL()
	}

	// Write confirmation prompt to the last footer (or status) row.
	ch.screen.GoXY(1, promptRow)
	ch.screen.ClearEOL()
	ch.screen.WriteDirect(" ") // 1 col indent
	ch.screen.WriteDirectProcessed(ch.abortText)

	// Save cursor position for inline lightbar, hide cursor.
	ch.screen.WriteDirect("\x1b[s")
	ch.screen.WriteDirect("\x1b[?25l")
	defer ch.screen.WriteDirect("\x1b[?25h")

	selectedIndex := 0 // 0=No (default), 1=Yes

	drawInline := func(sel int) {
		ch.screen.WriteDirect("\x1b[u")
		yesColor := lbUnselected
		noColor := lbUnselected
		if sel == 1 {
			yesColor = lbSelected
		} else {
			noColor = lbSelected
		}
		ch.screen.WriteDirect("  " + yesColor + " " + ch.yesText + " " + "\x1b[0m" + "  " + noColor + " " + ch.noText + " " + "\x1b[0m")
	}

	drawInline(selectedIndex)

	for {
		key, err := inputHandler.ReadKey()
		if err != nil {
			ch.screen.DisplayFooter()
			return false
		}

		switch key {
		case 'Y', 'y':
			return true // caller exits; footer not needed
		case 'N', 'n':
			ch.screen.DisplayFooter() // restore footer row before continuing
			return false
		case ' ', KeyEnter:
			if selectedIndex == 1 {
				return true
			}
			ch.screen.DisplayFooter()
			return false
		case KeyArrowLeft, KeyArrowRight:
			selectedIndex = 1 - selectedIndex
			drawInline(selectedIndex)
		}
	}
}

// HandleQuote handles the Quote command (CTRL-Q).
// Opens the split-pane quote picker: the message being composed stays on screen
// above a divider, the message being replied to is listed below it under a
// lightbar, and each selected line is inserted immediately. Everything paints
// inside the editing area, so the header and footer stay put.
// Returns the cursor position the editor should resume at.
func (ch *CommandHandler) HandleQuote(inputHandler *InputHandler, currentLine, currentCol int) (int, int) {
	if ch.quoteData == nil || len(ch.quoteData.Lines) == 0 {
		promptRow := ch.screen.PromptRow()
		ch.screen.GoXY(1, promptRow)
		ch.screen.ClearEOL()
		ch.screen.WriteDirectProcessed("|12You are not replying to anything! Press any key...")
		_, _ = inputHandler.ReadKey() // wait for any key
		return currentLine, currentCol
	}

	return ch.runQuoteMode(inputHandler, currentLine)
}

// processQuoteCodes processes ^N, ^T, ^D, ^W and ^I codes in quote strings
func (ch *CommandHandler) processQuoteCodes(text string) string {
	result := ""
	i := 0
	for i < len(text) {
		if text[i] == '^' && i+1 < len(text) {
			code := text[i+1]
			switch code {
			case 'N', 'n':
				if ch.quoteData.IsAnon {
					result += "Anonymous"
				} else {
					result += ch.quoteData.From
				}
				i += 2
			case 'I', 'i':
				result += quoteInitials(ch.quoteAuthor())
				i += 2
			case 'T', 't':
				result += ch.quoteData.Title
				i += 2
			case 'D', 'd':
				result += ch.quoteData.Date
				i += 2
			case 'W', 'w':
				result += ch.quoteData.Time
				i += 2
			default:
				result += string(text[i])
				i++
			}
		} else {
			result += string(text[i])
			i++
		}
	}
	return result
}

// filterPipeCodes removes pipe codes from quoted text if needed
func (ch *CommandHandler) filterPipeCodes(text string) string {
	// For now, always filter pipe codes from quoted text
	result := text
	i := len(result) - 2
	for i >= 0 {
		if result[i] == '|' && i+2 < len(result) {
			// Check if next two chars are digits
			if result[i+1] >= '0' && result[i+1] <= '9' && result[i+2] >= '0' && result[i+2] <= '9' {
				result = result[:i] + result[i+3:]
			}
		}
		i--
	}
	return result
}

// processForBuffer processes pipe codes to ANSI escape sequences for buffer storage
// This is needed because buffer content is displayed without pipe code processing
func (ch *CommandHandler) processForBuffer(text string) string {
	// Convert pipe codes to ANSI escape sequences
	processed := ansi.ReplacePipeCodes([]byte(text))
	return string(processed)
}

// HandleHelp handles the /H (help) command
// Displays the help screen
func (ch *CommandHandler) HandleHelp(inputHandler *InputHandler) {
	// Try to load EDITHELP.ANS file
	helpPath := filepath.Join(ch.menuSetPath, "ansi", "EDITHELP.ANS")
	helpContent, err := ansi.GetAnsiFileContent(helpPath)

	ch.screen.ClearScreen()

	if err == nil {
		// Display the help file
		ch.screen.WriteDirect(string(helpContent))
	} else {
		// Display built-in help
		ch.displayBuiltInHelp()
	}

	// Wait for key press
	ch.screen.GoXY(1, ch.screen.termHeight)
	ch.screen.WriteDirectProcessed("|15Press any key to continue...")
	_, _ = inputHandler.ReadKey() // wait for any key
}

// displayBuiltInHelp displays built-in help text
func (ch *CommandHandler) displayBuiltInHelp() {
	help := `|15Full Screen Message Editor Help|07

|11Navigation Commands:|07
  Ctrl+E or Up Arrow     - Move up one line
  Ctrl+X or Down Arrow   - Move down one line
  Ctrl+S or Left Arrow   - Move left one character
  Ctrl+D or Right Arrow  - Move right one character
  Ctrl+W or Home         - Move to start of line
  Ctrl+P or End          - Move to end of line
  Ctrl+R or Page Up      - Scroll up one page
  Ctrl+C or Page Down    - Scroll down one page
  Ctrl+F                 - Move right one word

|11Edit Commands:|07
  Ctrl+V or Insert       - Toggle Insert/Overwrite mode
  Ctrl+G or Delete       - Delete character at cursor
  Ctrl+T                 - Delete word to the right
  Ctrl+Y                 - Delete current line
  Ctrl+J                 - Join current line with next
  Ctrl+N                 - Split line at cursor
  Ctrl+B                 - Reformat paragraph
  Ctrl+L                 - Redraw screen
  Backspace              - Delete character to the left
  Tab                    - Insert tab (spaces)

|11Special Commands:|07
  Ctrl+Z                 - Save message and exit
  Ctrl+A                 - Abort message (with confirmation)
  Ctrl+Q                 - Quote previous message (when replying)
  Escape                 - Option menu (Save/Abort/Edit/Help/Quote)

|11Quote Mode (Ctrl+Q when replying):|07
  Up/Down                - Move the bar over the message you are quoting
  PgUp/PgDn, Home/End    - Jump through the message
  Space or Enter         - Add the highlighted line to your reply
  Backspace              - Remove the line you added last
  Tab                    - Switch between your reply and the quoted message
  Escape                 - Leave quote mode and carry on writing

|11Word Wrapping:|07
  Lines automatically wrap at 79 characters.
  Words are kept together when wrapping.
  Use Ctrl+B to reformat paragraphs.

`
	ch.screen.WriteDirectProcessed(help)
}

// HandleView handles the /V (view) command
// Displays the current message (not fully implemented)
func (ch *CommandHandler) HandleView(inputHandler *InputHandler) {
	ch.screen.ClearScreen()

	// Display message
	ch.screen.WriteDirectProcessed("|15Current Message:|07\r\n\r\n")

	lineCount := ch.buffer.GetLineCount()
	for i := 1; i <= lineCount; i++ {
		line := ch.buffer.GetLine(i)
		ch.screen.WriteDirect(line + "\r\n")
	}

	// Wait for key press
	ch.screen.GoXY(1, ch.screen.termHeight)
	ch.screen.WriteDirectProcessed("|15Press any key to continue...")
	_, _ = inputHandler.ReadKey() // wait for any key
}

// ShowEscapeMenu displays a lightbar selection menu at PromptRow when Escape is pressed.
// Items: Save, Abort, Edit (continue), Help, Quote.
// Returns the selected CommandType, or CommandNone to continue editing.
func (ch *CommandHandler) ShowEscapeMenu(inputHandler *InputHandler) CommandType {
	type menuItem struct {
		label   string
		cmdType CommandType
	}
	items := []menuItem{
		{"Save", CommandSave},
		{"Abort", CommandAbort},
		{"Edit", CommandNone},
		{"Help", CommandHelp},
		{"Quote", CommandQuote},
	}

	promptRow := ch.screen.PromptRow()
	selectedIndex := 2 // Default: "Edit" (continue editing)

	ch.screen.WriteDirect("\x1b[?25l") // hide cursor
	defer ch.screen.WriteDirect("\x1b[?25h")

	drawMenu := func(sel int) {
		ch.screen.GoXY(1, promptRow)
		ch.screen.ClearEOL()
		ch.screen.WriteDirect(" " + footerTextColor + "Select an Option" + footerSpecialColor + ":" + footerTextColor + " ")
		for i, item := range items {
			if i > 0 {
				ch.screen.WriteDirect("  ") // 2 spaces between items
			}
			if i == sel {
				ch.screen.WriteDirect(lbSelected + " " + item.label + " " + "\x1b[0m")
			} else {
				ch.screen.WriteDirect(lbUnselected + " " + item.label + " " + "\x1b[0m")
			}
		}
	}

	drawMenu(selectedIndex)

	for {
		key, err := inputHandler.ReadKey()
		if err != nil {
			ch.screen.DisplayFooter()
			return CommandNone
		}
		switch key {
		case KeyArrowLeft:
			if selectedIndex > 0 {
				selectedIndex--
				drawMenu(selectedIndex)
			}
		case KeyArrowRight:
			if selectedIndex < len(items)-1 {
				selectedIndex++
				drawMenu(selectedIndex)
			}
		case KeyEnter, ' ':
			ch.screen.DisplayFooter()
			return items[selectedIndex].cmdType
		case KeyEsc:
			ch.screen.DisplayFooter()
			return CommandNone
		}
	}
}
