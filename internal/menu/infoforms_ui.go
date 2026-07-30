package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// fillInfoForm handles the interactive form fill-out process.
// Maps to V2's infoform(a:byte) procedure in OVERRET1.PAS:1238-1297.
func fillInfoForm(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	outputMode ansi.OutputMode, nodeNumber int, currentUser *user.User,
	formNum int, termWidth int, termHeight int) {

	tmpl, err := parseTemplateFile(e.RootConfigPath, formNum)
	if err != nil {
		wv(terminal, fmt.Sprintf("\r\n|04There isn't an information #%d form right now.\r\n", formNum), outputMode)
		return
	}

	// Check if already completed — prompt to replace but don't delete yet.
	// The old response is preserved until the new one is fully saved (atomic rename).
	if hasCompletedForm(e.RootConfigPath, currentUser.ID, formNum) {
		replaceYes, err := e.PromptYesNo(s, terminal,
			fmt.Sprintf("|07You have already filled out form #%d! Replace it? @", formNum),
			outputMode, nodeNumber, termWidth, termHeight, false)
		if err != nil || !replaceYes {
			return
		}
	}

	wv(terminal, "\r\n", outputMode)

	// Walk through template: display text segments, collect input at field markers
	answers := make([]string, 0, len(tmpl.Fields))

	for i, field := range tmpl.Fields {
		// Display the text segment before this field
		segment := ""
		if i < len(tmpl.Segments) {
			segment = prepareSegment(tmpl.Segments[i])
			wv(terminal, segment, outputMode)
		}

		// Collect user input — loop on required fields until non-empty
		for {
			answer, err := readLineFromSessionIH(s, terminal)
			if err != nil {
				// User disconnected — don't save partial form
				return
			}

			// Enforce max length if set (rune-aware to avoid breaking multi-byte UTF-8)
			if field.MaxLen > 0 {
				runes := []rune(answer)
				if len(runes) > field.MaxLen {
					answer = string(runes[:field.MaxLen])
				}
			}

			if field.Required && strings.TrimSpace(answer) == "" {
				wv(terminal, "\r\n|04This field is required.|07\r\n", outputMode)
				// Re-display the segment so the prompt appears again
				if segment != "" {
					wv(terminal, segment, outputMode)
				}
				continue
			}

			answers = append(answers, answer)
			break
		}
	}

	// Display trailing text segment
	if len(tmpl.Segments) > len(tmpl.Fields) {
		wv(terminal, prepareSegment(tmpl.Segments[len(tmpl.Fields)]), outputMode)
	}

	// Save the response
	resp := &InfoFormResponse{
		UserID:      currentUser.ID,
		Handle:      currentUser.Handle,
		FormNum:     formNum,
		FilledOutAt: time.Now().UTC(),
		Answers:     answers,
	}

	infoformsMu.Lock()
	saveErr := saveInfoFormResponse(e.RootConfigPath, resp)
	infoformsMu.Unlock()

	if saveErr != nil {
		slog.Error("failed to save infoform response", "node", nodeNumber, "error", saveErr)
		wv(terminal, "\r\n|04Error saving your form.\r\n", outputMode)
		return
	}

	slog.Info("completed infoform", "node", nodeNumber, "handle", currentUser.Handle, "form", formNum)
	wv(terminal, "\r\n|10Form completed!\r\n", outputMode)
}

// showInfoForm displays a user's completed form response.
// Maps to V2's showinfoforms(uname, a) in SUBSOVR.PAS:213-284.
func showInfoForm(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode,
	userID int, formNum int, termHeight int) {

	resp, err := loadInfoFormResponse(e.RootConfigPath, userID, formNum)
	if err != nil {
		wv(terminal, "\r\n|04Error loading form response.\r\n", outputMode)
		return
	}
	if resp == nil {
		wv(terminal, "\r\n|07That user has no information form.\r\n", outputMode)
		return
	}

	tmpl, err := parseTemplateFile(e.RootConfigPath, formNum)
	if err != nil {
		wv(terminal, fmt.Sprintf("\r\n|07Infoform #%d is blank.\r\n", formNum), outputMode)
		return
	}

	// Paging: track lines written and pause per screenful.
	linesPerPage := termHeight - 3 // leave room for header + more prompt
	if linesPerPage < 5 {
		linesPerPage = 5
	}
	lineCount := 0

	morePrompt := e.LoadedStrings.FileMorePrompt
	if morePrompt == "" {
		morePrompt = "\r\n|08--- |15More|08 --- |07[Enter]=Continue [Q]=Stop"
	}

	// wvPaged writes a string and tracks newlines for paging.
	// Returns false if the user pressed Q to abort.
	aborted := false
	wvPaged := func(text string) {
		if aborted {
			return
		}
		// Count newlines in the text to track lines written
		lines := strings.Split(text, "\r\n")
		for j, line := range lines {
			if j > 0 {
				// Each \r\n is a new line
				lineCount++
				if lineCount >= linesPerPage {
					if !pauseMore(s, terminal, outputMode, morePrompt) {
						aborted = true
						return
					}
					lineCount = 0
				}
			}
			wv(terminal, line, outputMode)
			if j < len(lines)-1 {
				wv(terminal, "\r\n", outputMode)
			}
		}
	}

	// Show completion date (V2: first line of stored message)
	wvPaged(fmt.Sprintf("\r\n|08Filled out on: %s\r\n\r\n",
		resp.FilledOutAt.Format("01/02/2006 at 03:04 PM")))

	// Replay template with answers interpolated at * markers
	answerIdx := 0
	for i := range tmpl.Fields {
		if aborted {
			break
		}
		// Display text segment
		if i < len(tmpl.Segments) {
			wvPaged(prepareSegment(tmpl.Segments[i]))
		}

		// Display answer (or "No answer")
		if answerIdx < len(resp.Answers) {
			answer := resp.Answers[answerIdx]
			if strings.TrimSpace(answer) == "" {
				wvPaged("|08No answer")
			} else {
				// Escape pipe codes in stored answers to prevent pipe-code injection
				sanitized := strings.ReplaceAll(answer, "|", "||")
				wvPaged("|15" + sanitized)
			}
			answerIdx++
		} else {
			wvPaged("|08No answer")
		}
	}

	if !aborted {
		// Display trailing text
		if len(tmpl.Segments) > len(tmpl.Fields) {
			wvPaged(prepareSegment(tmpl.Segments[len(tmpl.Fields)]))
		}
		wvPaged("\r\n")
	}
}

// browseInfoForms shows an interactive infoform browser for a user.
// Used by both the admin and validate online user editors.
// Returns an error only if ReadKey encounters io.EOF (session closed).
func browseInfoForms(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	outputMode ansi.OutputMode, sel *user.User, ifCfg *InfoFormConfig,
	termWidth int, termHeight int) error {

	for {
		_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		wv(terminal, fmt.Sprintf("\r\n|15InfoForms for |11%s|07\r\n", sel.Handle), outputMode)
		wv(terminal, "|08"+strings.Repeat("-", 50)+"\r\n\r\n", outputMode)

		hasAnyForm := false
		for i := 0; i < 5; i++ {
			formNum := i + 1
			if !templateExists(e.RootConfigPath, formNum) {
				continue
			}
			hasAnyForm = true
			desc := ifCfg.Descriptions[i]
			if desc == "" {
				desc = fmt.Sprintf("Form #%d", formNum)
			}
			if hasCompletedForm(e.RootConfigPath, sel.ID, formNum) {
				wv(terminal, fmt.Sprintf("  |15%d|08. |15%-30s |10[Completed]\r\n", formNum, desc), outputMode)
			} else {
				wv(terminal, fmt.Sprintf("  |08%d|08. |07%-30s |04[Incomplete]\r\n", formNum, desc), outputMode)
			}
		}
		if !hasAnyForm {
			wv(terminal, "|07No infoform templates configured.\r\n", outputMode)
			e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
			return nil
		}

		wv(terminal, "\r\n|08Press |151-5|08 to view a form, |15Q|08 to return.|07\r\n", outputMode)

		key, err := getSessionIH(s).ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return err
			}
			return nil
		}
		if key == int('q') || key == int('Q') || key == int(editor.KeyEsc) {
			return nil
		}
		if key >= int('1') && key <= int('5') {
			formNum := key - int('0')
			if !templateExists(e.RootConfigPath, formNum) {
				continue
			}
			_ = terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			desc := ifCfg.Descriptions[formNum-1]
			if desc == "" {
				desc = fmt.Sprintf("Form #%d", formNum)
			}
			wv(terminal, fmt.Sprintf("\r\n|15%s|07\r\n", desc), outputMode)
			wv(terminal, "|08"+strings.Repeat("-", 50)+"\r\n", outputMode)
			if hasCompletedForm(e.RootConfigPath, sel.ID, formNum) {
				showInfoForm(e, s, terminal, outputMode, sel.ID, formNum, termHeight)
			} else {
				wv(terminal, "\r\n|04This form has not been completed.\r\n", outputMode)
			}
			e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
		}
	}
}
