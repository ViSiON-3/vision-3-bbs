package menu

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	term "golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// proposeField describes one editable field in the propose form.
type proposeField struct {
	label    string
	help     string
	value    string
	maxLen   int
	editable bool // false for display-only fields
}

// accessModes is the cycle list for the Access Mode field.
var accessModes = []string{"open", "approval", "closed"}

func runV3NetPropose(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, _ *user.UserMgr, currentUser *user.User, _ int, _ time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil || e.V3NetStatus == nil {
		return nil, "", nil
	}

	svc := e.V3NetStatus

	// Pick the network. If args specifies one, use it; otherwise use first leaf network.
	var network string
	if args != "" {
		network = args
	} else {
		nets := svc.LeafNetworks()
		if len(nets) == 0 {
			return proposeShowMessage(e, terminal, s, outputMode, termWidth, termHeight,
				"|04No V3Net subscriptions configured.|07")
		}
		network = nets[0]
	}

	// Form fields.
	fields := []proposeField{
		{label: "Network", value: network, editable: false},
		{label: "Area Tag", help: "prefix.name (e.g. fel.general)", maxLen: 34, editable: true},
		{label: "Name", help: "Display name for the area", maxLen: 40, editable: true},
		{label: "Description", help: "Short description", maxLen: 60, editable: true},
		{label: "Language", value: "en", maxLen: 8, editable: true},
		{label: "Access Mode", value: "open", editable: true},
		{label: "Allow ANSI", value: "Yes", editable: true},
	}

	const (
		fldTag = iota + 1
		fldName
		fldDesc
		fldLang
		fldAccess
		fldANSI
	)

	ih := getSessionIH(s)
	selectedField := fldTag // start on Area Tag
	statusMsg := ""

	// Layout constants.
	formStartRow := 5   // first field row
	fieldLabelCol := 3  // label column
	fieldValueCol := 18 // value column

	renderForm := func() {
		var buf strings.Builder
		buf.WriteString(ansi.ClearScreen())

		// Row 1: Header.
		buf.WriteString(ansi.MoveCursor(1, 1))
		header := fmt.Sprintf("|12V3Net: Propose New Area|07  |08(%s)|07", network)
		buf.Write(ansi.ReplacePipeCodes([]byte(header)))

		// Row 2: Separator.
		buf.WriteString(ansi.MoveCursor(2, 1))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08" + strings.Repeat("\xC4", termWidth-1) + "|07")))

		// Row 3: Instructions.
		buf.WriteString(ansi.MoveCursor(3, 1))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08 Fill in the fields below. Tag format: prefix.name (lowercase, 1-8.1-24)|07")))

		// Fields.
		for i, f := range fields {
			row := formStartRow + i
			buf.WriteString(ansi.MoveCursor(row, fieldLabelCol))

			isSelected := i == selectedField
			if isSelected {
				buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|15>|11 %-13s|07", f.label))))
			} else if !f.editable {
				buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|08  %-13s|07", f.label))))
			} else {
				buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|03  %-13s|07", f.label))))
			}

			buf.WriteString(ansi.MoveCursor(row, fieldValueCol))
			val := f.value
			if val == "" {
				if f.help != "" && !isSelected {
					buf.Write(ansi.ReplacePipeCodes([]byte("|08" + f.help + "|07")))
				}
			} else {
				if isSelected {
					buf.Write(ansi.ReplacePipeCodes([]byte("|15" + val + "|07")))
				} else {
					buf.Write(ansi.ReplacePipeCodes([]byte("|07" + val)))
				}
			}
		}

		// Separator before help.
		helpRow := formStartRow + len(fields) + 1
		buf.WriteString(ansi.MoveCursor(helpRow, 1))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08" + strings.Repeat("\xC4", termWidth-1) + "|07")))

		// Help line.
		buf.WriteString(ansi.MoveCursor(helpRow+1, 1))
		help := "|08 [|15Enter|08] Edit  [|15Tab|08] Next  [|15Space|08] Toggle  [|15Ctrl-S|08] Submit  [|15Q|08] Quit|07"
		buf.Write(ansi.ReplacePipeCodes([]byte(help)))

		// Status line.
		buf.WriteString(ansi.MoveCursor(helpRow+2, 1))
		buf.WriteString("\x1b[2K") // clear line
		if statusMsg != "" {
			buf.Write(ansi.ReplacePipeCodes([]byte(statusMsg)))
		}

		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)
	}

	// editStringField lets the user type into a string field inline.
	editStringField := func(fieldIdx int) {
		f := &fields[fieldIdx]
		if !f.editable {
			return
		}

		row := formStartRow + fieldIdx

		// Clear the value area and show cursor.
		var buf strings.Builder
		buf.WriteString(ansi.MoveCursor(row, fieldValueCol))
		buf.WriteString("\x1b[K")    // clear to end of line
		buf.WriteString("\x1b[?25h") // show cursor
		buf.Write(ansi.ReplacePipeCodes([]byte("|15")))
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

		input := []byte(f.value)
		cursorIdx := len(input)

		// Render current value.
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(row, fieldValueCol)+string(input)), outputMode)

		for {
			key, err := ih.ReadKey()
			if err != nil {
				if errors.Is(err, editor.ErrIdleTimeout) || errors.Is(err, io.EOF) {
					terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
					return
				}
				break
			}

			switch key {
			case '\r', '\n':
				f.value = string(input)
				terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
				return
			case editor.KeyEsc:
				// Cancel edit, keep old value.
				terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
				return
			case editor.KeyBackspace, editor.KeyDelete:
				if cursorIdx > 0 {
					input = append(input[:cursorIdx-1], input[cursorIdx:]...)
					cursorIdx--
				}
			case editor.KeyDeleteKey:
				if cursorIdx < len(input) {
					input = append(input[:cursorIdx], input[cursorIdx+1:]...)
				}
			case editor.KeyArrowLeft:
				if cursorIdx > 0 {
					cursorIdx--
				}
			case editor.KeyArrowRight:
				if cursorIdx < len(input) {
					cursorIdx++
				}
			case editor.KeyHome:
				cursorIdx = 0
			case editor.KeyEnd:
				cursorIdx = len(input)
			default:
				if key >= 32 && key < 127 && len(input) < f.maxLen {
					input = append(input[:cursorIdx], append([]byte{byte(key)}, input[cursorIdx:]...)...)
					cursorIdx++
				}
			}

			// Redraw field value.
			line := ansi.MoveCursor(row, fieldValueCol) + "\x1b[K" + string(input)
			terminalio.WriteProcessedBytes(terminal, []byte(line), outputMode)
			// Position cursor.
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(row, fieldValueCol+cursorIdx)), outputMode)
		}
	}

	// Hide cursor during form navigation.
	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
	defer terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)

	renderForm()

	for {
		key, err := ih.ReadKey()
		if err != nil {
			if errors.Is(err, editor.ErrIdleTimeout) {
				return nil, "LOGOFF", editor.ErrIdleTimeout
			}
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", err
		}

		statusMsg = ""

		switch key {
		case editor.KeyArrowUp:
			selectedField--
			if selectedField < fldTag {
				selectedField = fldANSI
			}
			renderForm()

		case editor.KeyArrowDown, '\t':
			selectedField++
			if selectedField > fldANSI {
				selectedField = fldTag
			}
			renderForm()

		case '\r', '\n':
			// Edit the current field.
			f := &fields[selectedField]
			if !f.editable {
				continue
			}
			switch selectedField {
			case fldAccess:
				// Cycle access mode.
				f.value = cycleAccessMode(f.value)
				renderForm()
			case fldANSI:
				// Toggle.
				if f.value == "Yes" {
					f.value = "No"
				} else {
					f.value = "Yes"
				}
				renderForm()
			default:
				editStringField(selectedField)
				renderForm()
			}

		case ' ':
			// Toggle/cycle for Access Mode and Allow ANSI.
			f := &fields[selectedField]
			switch selectedField {
			case fldAccess:
				f.value = cycleAccessMode(f.value)
				renderForm()
			case fldANSI:
				if f.value == "Yes" {
					f.value = "No"
				} else {
					f.value = "Yes"
				}
				renderForm()
			}

		case editor.KeyCtrlS:
			// Submit proposal.
			tag := strings.TrimSpace(fields[fldTag].value)
			name := strings.TrimSpace(fields[fldName].value)

			if tag == "" {
				statusMsg = "|04Area Tag is required.|07"
				renderForm()
				continue
			}
			if err := protocol.ValidateAreaTag(tag); err != nil {
				statusMsg = fmt.Sprintf("|04%s|07", err)
				renderForm()
				continue
			}
			if name == "" {
				statusMsg = "|04Name is required.|07"
				renderForm()
				continue
			}

			req := protocol.AreaProposalRequest{
				Tag:         tag,
				Name:        name,
				Description: strings.TrimSpace(fields[fldDesc].value),
				Language:    strings.TrimSpace(fields[fldLang].value),
				AccessMode:  fields[fldAccess].value,
				AllowANSI:   fields[fldANSI].value == "Yes",
			}
			if req.Language == "" {
				req.Language = "en"
			}

			statusMsg = "|08Submitting proposal...|07"
			renderForm()

			resp, submitErr := svc.ProposeArea(network, req)
			if submitErr != nil {
				statusMsg = fmt.Sprintf("|04Error: %s|07", submitErr)
				renderForm()
				continue
			}

			statusMsg = fmt.Sprintf("|10Proposal submitted! ID: %s  Status: %s|07", resp.ProposalID, resp.Status)
			renderForm()

			// Wait for a keypress, then return.
			if _, err := ih.ReadKey(); err != nil {
				return nil, "", err
			}
			return nil, "", nil

		case 'q', 'Q', editor.KeyEsc:
			return nil, "", nil
		}
	}
}

// cycleAccessMode advances to the next access mode in the list.
func cycleAccessMode(current string) string {
	for i, m := range accessModes {
		if m == current {
			return accessModes[(i+1)%len(accessModes)]
		}
	}
	return accessModes[0]
}

// proposeShowMessage displays a single message with a pause prompt.
func proposeShowMessage(e *MenuExecutor, terminal *term.Terminal, s ssh.Session, outputMode ansi.OutputMode, termWidth, termHeight int, msg string) (*user.User, string, error) {
	var buf strings.Builder
	buf.WriteString(ansi.ClearScreen())
	buf.WriteString(ansi.MoveCursor(1, 1))
	buf.Write(ansi.ReplacePipeCodes([]byte("|12V3Net: Propose New Area|07")))
	buf.WriteString(ansi.MoveCursor(2, 1))
	buf.Write(ansi.ReplacePipeCodes([]byte("|08" + strings.Repeat("\xC4", termWidth-1) + "|07")))
	buf.WriteString(ansi.MoveCursor(4, 3))
	buf.Write(ansi.ReplacePipeCodes([]byte(msg)))
	terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	if err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight); err != nil {
		return nil, "", err
	}
	return nil, "", nil
}
