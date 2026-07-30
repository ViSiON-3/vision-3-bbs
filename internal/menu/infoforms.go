package menu

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runInfoForms is the main infoforms menu — lists available forms and lets users fill/view them.
// Maps to V2's Infoforms procedure in RUMORS.PAS:117-210.
func runInfoForms(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return currentUser, "", nil
	}

	slog.Debug("running INFOFORMS", "node", nodeNumber, "handle", currentUser.Handle)

	infoformsMu.Lock()
	cfg, err := loadInfoFormConfig(e.RootConfigPath)
	infoformsMu.Unlock()
	if err != nil {
		wv(terminal, "\r\n|04Error loading infoforms config.\r\n", outputMode)
		return currentUser, "", nil
	}

	isNewUser := !currentUser.Validated

	for {
		// Show available forms listing
		wv(terminal, "\x1b[2J\x1b[H", outputMode) // Clear screen

		hasAnyForms := false
		wv(terminal, "\r\n|11 #  Description                    Required   Status\r\n", outputMode)
		wv(terminal, "|08"+strings.Repeat("\xc4", 70)+"\r\n", outputMode)

		for i := 0; i < 5; i++ {
			formNum := i + 1
			if !templateExists(e.RootConfigPath, formNum) {
				continue
			}
			if cfg.MinLevels[i] > currentUser.AccessLevel {
				continue
			}

			hasAnyForms = true
			desc := cfg.Descriptions[i]
			if desc == "" {
				desc = "\xfa No Description \xfa"
			}

			reqStr := "Optional"
			if isFormRequired(cfg, formNum) {
				reqStr = "Required"
			}

			status := "|04Incomplete!"
			if hasCompletedForm(e.RootConfigPath, currentUser.ID, formNum) {
				status = "|10Completed.."
			}

			wv(terminal, fmt.Sprintf("|03%-4d|07%-35s|09%-11s%s|07\r\n",
				formNum, truncateRunes(desc, 34), reqStr, status), outputMode)
		}

		if !hasAnyForms {
			wv(terminal, "|07No infoforms available.\r\n", outputMode)
			return currentUser, "", nil
		}

		wv(terminal, "\r\n", outputMode)

		// Prompt
		var prompt string
		if isNewUser {
			prompt = e.LoadedStrings.NewInfoFormPrompt
			if prompt == "" {
				prompt = "|08N|07e|15wuser |08F|07o|15rms |09 |01(|09Q|01)uit or |09#|08: "
			}
		} else {
			prompt = e.LoadedStrings.InfoformPrompt
			if prompt == "" {
				prompt = "|08I|07n|15foForms|09 |01(|09V|01)iew (|09Q|01)uit or |09#|08: "
			}
		}

		wv(terminal, prompt, outputMode)
		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			return currentUser, "", nil
		}
		input = strings.TrimSpace(input)
		if input == "" {
			input = "Q"
		}

		upper := strings.ToUpper(input)

		if upper == "Q" {
			// Check all required forms are completed before allowing quit
			allDone := true
			for i := 0; i < 5; i++ {
				formNum := i + 1
				if isFormRequired(cfg, formNum) && templateExists(e.RootConfigPath, formNum) {
					if !hasCompletedForm(e.RootConfigPath, currentUser.ID, formNum) {
						wv(terminal, fmt.Sprintf("|05You still must complete Infoform #%d\r\n", formNum), outputMode)
						allDone = false
					}
				}
			}
			if allDone {
				return currentUser, "", nil
			}
			continue
		}

		if upper == "V" && !isNewUser {
			// View completed form
			viewPrompt := e.LoadedStrings.ViewWhichForm
			if viewPrompt == "" {
				viewPrompt = "|09View which |08F|07o|15rm? (|07#|15) |09:"
			}
			wv(terminal, viewPrompt, outputMode)
			viewInput, err := readLineFromSessionIH(s, terminal)
			if err != nil {
				return currentUser, "", nil
			}
			viewNum, nerr := strconv.Atoi(strings.TrimSpace(viewInput))
			if nerr != nil || viewNum < 1 || viewNum > 5 {
				wv(terminal, "\r\n|04Invalid form number.\r\n", outputMode)
				continue
			}
			if !templateExists(e.RootConfigPath, viewNum) {
				wv(terminal, "\r\n|04That form doesn't exist.\r\n", outputMode)
				continue
			}
			showInfoForm(e, s, terminal, outputMode, currentUser.ID, viewNum, termHeight)
			e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
			continue
		}

		// Try as form number to fill out
		formNum, nerr := strconv.Atoi(input)
		if nerr != nil || formNum < 1 || formNum > 5 {
			continue
		}
		if !templateExists(e.RootConfigPath, formNum) || cfg.MinLevels[formNum-1] > currentUser.AccessLevel {
			wv(terminal, "\r\n|04Sorry, not a valid Infoform!\r\n", outputMode)
			e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
			continue
		}

		// Fill out the form
		fillInfoForm(e, s, terminal, outputMode, nodeNumber, currentUser, formNum, termWidth, termHeight)
	}
}
