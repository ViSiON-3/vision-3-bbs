package menu

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stlalpha/vision3/internal/ansi"
	"github.com/stlalpha/vision3/internal/editor"
	"github.com/stlalpha/vision3/internal/terminalio"
	"github.com/stlalpha/vision3/internal/user"
	"github.com/stlalpha/vision3/internal/version"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// InfoFormConfig holds sysop-configured infoform settings.
// Maps to V2's Cfg.InfoformStr[1..5], Cfg.Infoformlvl[1..5], Cfg.RequiredForms.
type InfoFormConfig struct {
	Descriptions [5]string `json:"descriptions"` // Form descriptions (V2: InfoformStr)
	MinLevels    [5]int    `json:"min_levels"`    // Minimum access level per form (V2: Infoformlvl)
	RequiredForms string   `json:"required_forms"` // Which forms are required, e.g. "15" = forms 1 and 5
}

// InfoFormResponse holds a user's completed answers for a specific form.
// Maps to V2's FORMS.TXT/FORMS.MAP entries.
type InfoFormResponse struct {
	UserID      int       `json:"user_id"`
	Handle      string    `json:"handle"`
	FormNum     int       `json:"form_num"`
	FilledOutAt time.Time `json:"filled_out_at"`
	Answers     []string  `json:"answers"` // One entry per * field in the template
}

var infoformsMu sync.Mutex

// infoformsDataDir returns the path to the infoforms data directory.
func infoformsDataDir(rootConfigPath string) string {
	return filepath.Join(rootConfigPath, "..", "data", "infoforms")
}

// infoformsConfigPath returns the path to the infoforms config file.
func infoformsConfigPath(rootConfigPath string) string {
	return filepath.Join(infoformsDataDir(rootConfigPath), "config.json")
}

// infoformsTemplatePath returns the path to a form template file.
func infoformsTemplatePath(rootConfigPath string, formNum int) string {
	return filepath.Join(infoformsDataDir(rootConfigPath), "templates", fmt.Sprintf("form_%d.txt", formNum))
}

// infoformsResponsePath returns the path to a user's response file.
func infoformsResponsePath(rootConfigPath string, userID int, formNum int) string {
	return filepath.Join(infoformsDataDir(rootConfigPath), "responses", fmt.Sprintf("%d_%d.json", userID, formNum))
}

// loadInfoFormConfig loads the infoforms configuration.
func loadInfoFormConfig(rootConfigPath string) (*InfoFormConfig, error) {
	data, err := os.ReadFile(infoformsConfigPath(rootConfigPath))
	if err != nil {
		if os.IsNotExist(err) {
			// Return defaults matching V2 (CONFIG.PAS:73-78)
			return &InfoFormConfig{
				Descriptions: [5]string{"New User Application", "", "", "", ""},
				MinLevels:    [5]int{0, 0, 0, 0, 0},
				RequiredForms: "",
			}, nil
		}
		return nil, fmt.Errorf("read infoforms config: %w", err)
	}
	var cfg InfoFormConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse infoforms config: %w", err)
	}
	return &cfg, nil
}

// saveInfoFormConfig saves the infoforms configuration.
func saveInfoFormConfig(rootConfigPath string, cfg *InfoFormConfig) error {
	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal infoforms config: %w", err)
	}
	fp := infoformsConfigPath(rootConfigPath)
	if err := os.MkdirAll(filepath.Dir(fp), 0755); err != nil {
		return fmt.Errorf("create infoforms config directory: %w", err)
	}
	return os.WriteFile(fp, data, 0644)
}

// loadInfoFormResponse loads a user's response for a specific form.
func loadInfoFormResponse(rootConfigPath string, userID int, formNum int) (*InfoFormResponse, error) {
	data, err := os.ReadFile(infoformsResponsePath(rootConfigPath, userID, formNum))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No response yet
		}
		return nil, fmt.Errorf("read infoform response: %w", err)
	}
	var resp InfoFormResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse infoform response: %w", err)
	}
	return &resp, nil
}

// saveInfoFormResponse saves a user's response for a specific form.
// Uses temp file + rename to prevent torn reads by concurrent sessions.
// Note: os.Rename is atomic on Unix/POSIX but not guaranteed atomic on Windows.
// On Windows, concurrent readers may briefly see a missing file during the rename.
func saveInfoFormResponse(rootConfigPath string, resp *InfoFormResponse) error {
	data, err := json.MarshalIndent(resp, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal infoform response: %w", err)
	}
	fp := infoformsResponsePath(rootConfigPath, resp.UserID, resp.FormNum)
	dir := filepath.Dir(fp)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create infoforms response directory: %w", err)
	}
	// Write to temp file then rename for atomic update
	tmp, err := os.CreateTemp(dir, ".infoform-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, fp); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// deleteInfoFormResponse deletes a user's response for a specific form.
func deleteInfoFormResponse(rootConfigPath string, userID int, formNum int) error {
	fp := infoformsResponsePath(rootConfigPath, userID, formNum)
	err := os.Remove(fp)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete infoform response: %w", err)
	}
	return nil
}

// hasCompletedForm checks if a user has completed a specific form.
// Uses file existence check — no User struct field needed.
func hasCompletedForm(rootConfigPath string, userID int, formNum int) bool {
	_, err := os.Stat(infoformsResponsePath(rootConfigPath, userID, formNum))
	return err == nil
}

// templateExists checks if a form template file exists.
func templateExists(rootConfigPath string, formNum int) bool {
	_, err := os.Stat(infoformsTemplatePath(rootConfigPath, formNum))
	return err == nil
}

// isFormRequired checks if a form number is in the required forms string.
// V2 pattern: RequiredForms is a string like "15" meaning forms 1 and 5 required.
func isFormRequired(cfg *InfoFormConfig, formNum int) bool {
	return strings.Contains(cfg.RequiredForms, strconv.Itoa(formNum))
}

// expandInfoformCodes replaces infoform-specific placeholders in template text.
// |VN is replaced with the BBS version number.
func expandInfoformCodes(s string) string {
	return strings.ReplaceAll(s, "|VN", version.Number)
}

// prepareSegment normalizes newlines and expands infoform codes for terminal display.
func prepareSegment(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "\r\n")
	return expandInfoformCodes(s)
}

// parseTemplate reads a form template and returns the text segments and field metadata.
// Template format (from V2):
//   - Plain text displayed to user character by character
//   - * = pause for user input (one answer field, optional)
//   - *! = pause for user input (required — user cannot leave blank)
//   - |B<n>; = set max input buffer length to n characters for next field
type templateField struct {
	MaxLen   int  // Max input length (0 = default/unlimited)
	Required bool // If true, user cannot leave this field blank
}

type parsedTemplate struct {
	Segments []string        // Text segments between input fields
	Fields   []templateField // Field metadata (one per * marker)
}

// parseTemplateFile reads and parses a form template file.
func parseTemplateFile(rootConfigPath string, formNum int) (*parsedTemplate, error) {
	data, err := os.ReadFile(infoformsTemplatePath(rootConfigPath, formNum))
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}

	tmpl := &parsedTemplate{}
	var currentSegment strings.Builder
	currentMaxLen := 0

	i := 0
	for i < len(data) {
		ch := data[i]

		if ch == '*' {
			// Input field marker: * = optional, *! = required
			required := false
			if i+1 < len(data) && data[i+1] == '!' {
				required = true
				i++ // consume the '!'
			}
			tmpl.Segments = append(tmpl.Segments, currentSegment.String())
			currentSegment.Reset()
			tmpl.Fields = append(tmpl.Fields, templateField{MaxLen: currentMaxLen, Required: required})
			currentMaxLen = 0 // Reset for next field
			i++
			continue
		}

		if ch == '|' && i+1 < len(data) && (data[i+1] == 'B' || data[i+1] == 'b') {
			// |B<n>; — buffer length control code
			j := i + 2
			var numStr strings.Builder
			for j < len(data) && data[j] != ';' {
				numStr.WriteByte(data[j])
				j++
			}
			if j < len(data) && data[j] == ';' {
				if n, err := strconv.Atoi(numStr.String()); err == nil && n >= 1 && n <= 255 {
					currentMaxLen = n
				}
				i = j + 1
				continue
			}
			// Not a valid |B code, write literally
			currentSegment.WriteByte(ch)
			i++
			continue
		}

		currentSegment.WriteByte(ch)
		i++
	}

	// Add trailing text segment
	tmpl.Segments = append(tmpl.Segments, currentSegment.String())

	return tmpl, nil
}

// runInfoForms is the main infoforms menu — lists available forms and lets users fill/view them.
// Maps to V2's Infoforms procedure in RUMORS.PAS:117-210.
func runInfoForms(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return currentUser, "", nil
	}

	log.Printf("DEBUG: Node %d: Running INFOFORMS for user %s", nodeNumber, currentUser.Handle)

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
		log.Printf("ERROR: Node %d: Failed to save infoform response: %v", nodeNumber, saveErr)
		wv(terminal, "\r\n|04Error saving your form.\r\n", outputMode)
		return
	}

	log.Printf("INFO: Node %d: %s completed infoform #%d", nodeNumber, currentUser.Handle, formNum)
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

// runInfoFormView lets a user view their own completed forms.
// Maps to V2's ShowInfoForms call pattern.
func runInfoFormView(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return currentUser, "", nil
	}

	viewPrompt := e.LoadedStrings.ViewWhichForm
	if viewPrompt == "" {
		viewPrompt = "|09View which |08F|07o|15rm? (|07#|15) |09:"
	}
	wv(terminal, viewPrompt, outputMode)
	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", nil
	}
	formNum, nerr := strconv.Atoi(strings.TrimSpace(input))
	if nerr != nil || formNum < 1 || formNum > 5 {
		wv(terminal, "\r\n|04Invalid form number.\r\n", outputMode)
		return currentUser, "", nil
	}

	showInfoForm(e, s, terminal, outputMode, currentUser.ID, formNum, termHeight)
	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
	return currentUser, "", nil
}

// runInfoFormHunt lets sysops browse all users' completed forms.
// Maps to V2's InfoFormHunt in MAINMENU.PAS:1161.
func runInfoFormHunt(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return currentUser, "", nil
	}

	isSysop := currentUser.AccessLevel >= 255
	if !isSysop {
		wv(terminal, "\r\n|04Access denied.\r\n", outputMode)
		return currentUser, "", nil
	}

	wv(terminal, "\r\n|07Show which infoform? |15(1-5)|07: ", outputMode)
	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", nil
	}
	formNum, nerr := strconv.Atoi(strings.TrimSpace(input))
	if nerr != nil || formNum < 1 || formNum > 5 {
		wv(terminal, "\r\n|04Invalid form number.\r\n", outputMode)
		return currentUser, "", nil
	}

	if !templateExists(e.RootConfigPath, formNum) {
		wv(terminal, "\r\n|04That form template doesn't exist.\r\n", outputMode)
		return currentUser, "", nil
	}

	wv(terminal, "\r\n|15Showing All Forms #"+strconv.Itoa(formNum)+"\r\n", outputMode)
	wv(terminal, "|08"+strings.Repeat("\xc4", 50)+"\r\n", outputMode)

	// Scan responses directory for this form number
	respDir := filepath.Join(infoformsDataDir(e.RootConfigPath), "responses")
	entries, err := os.ReadDir(respDir)
	if err != nil {
		if os.IsNotExist(err) {
			wv(terminal, "|07No responses found.\r\n", outputMode)
			return currentUser, "", nil
		}
		wv(terminal, "\r\n|04Error reading responses.\r\n", outputMode)
		return currentUser, "", nil
	}

	suffix := fmt.Sprintf("_%d.json", formNum)
	found := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		// Parse userID from filename
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		uid, uerr := strconv.Atoi(parts[0])
		if uerr != nil {
			continue
		}

		resp, rerr := loadInfoFormResponse(e.RootConfigPath, uid, formNum)
		if rerr != nil || resp == nil {
			continue
		}

		found++
		displayHandle := resp.Handle
		if u, ok := userManager.GetUserByID(uid); ok && u.Handle != "" {
			displayHandle = u.Handle
		}
		wv(terminal, fmt.Sprintf("\r\n|11%s\r\n", displayHandle), outputMode)
		showInfoForm(e, s, terminal, outputMode, uid, formNum, termHeight)
	}

	if found == 0 {
		wv(terminal, "|07No responses found.\r\n", outputMode)
	}

	e.holdScreen(s, terminal, outputMode, termWidth, termHeight)
	return currentUser, "", nil
}

// runInfoFormRequired checks and forces required forms during login sequence.
// Maps to V2's GETLOGIN.PAS:1592 required forms enforcement.
func runInfoFormRequired(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return currentUser, "", nil
	}

	// Only force required infoforms on new (unvalidated) users.
	// Existing validated users should not be prompted.
	if currentUser.Validated {
		return currentUser, "", nil
	}

	infoformsMu.Lock()
	cfg, err := loadInfoFormConfig(e.RootConfigPath)
	infoformsMu.Unlock()
	if err != nil {
		log.Printf("ERROR: Node %d: Failed to load infoforms config in required check: %v", nodeNumber, err)
		return currentUser, "", nil
	}

	for i := 0; i < 5; i++ {
		formNum := i + 1
		if !isFormRequired(cfg, formNum) {
			continue
		}
		if !templateExists(e.RootConfigPath, formNum) {
			continue
		}
		if hasCompletedForm(e.RootConfigPath, currentUser.ID, formNum) {
			continue
		}
		// Force fill out this required form
		fillInfoForm(e, s, terminal, outputMode, nodeNumber, currentUser, formNum, termWidth, termHeight)
		// Re-check: if form still not completed (save failed, user disconnected, etc.), block login
		if !hasCompletedForm(e.RootConfigPath, currentUser.ID, formNum) {
			log.Printf("WARN: Node %d: Required infoform #%d not completed by %s, blocking login",
				nodeNumber, formNum, currentUser.Handle)
			wv(terminal, fmt.Sprintf("\r\n|04Required form #%d was not completed. Disconnecting.\r\n", formNum), outputMode)
			return currentUser, "LOGOFF", nil
		}
	}

	return currentUser, "", nil
}

// runInfoFormNuke lets sysops delete all form responses for a specific user.
// Maps to V2's nuke all infoforms (MAINMENU.PAS:1580-1592).
func runInfoFormNuke(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return currentUser, "", nil
	}

	isSysop := currentUser.AccessLevel >= 255
	if !isSysop {
		wv(terminal, "\r\n|04Access denied.\r\n", outputMode)
		return currentUser, "", nil
	}

	wv(terminal, "\r\n|07Handle to nuke infoforms for: ", outputMode)
	input, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		return currentUser, "", nil
	}
	handle := strings.TrimSpace(input)
	if handle == "" {
		return currentUser, "", nil
	}

	// Look up user by handle
	targetUser, found := userManager.GetUser(handle)
	if !found {
		wv(terminal, "\r\n|04User not found.\r\n", outputMode)
		return currentUser, "", nil
	}

	nukeYes, err := e.PromptYesNo(s, terminal,
		fmt.Sprintf("|07Erase ALL info-forms for %s.. Are you sure? @", targetUser.Handle),
		outputMode, nodeNumber, termWidth, termHeight, false)
	if err != nil || !nukeYes {
		return currentUser, "", nil
	}

	infoformsMu.Lock()
	for i := 1; i <= 5; i++ {
		_ = deleteInfoFormResponse(e.RootConfigPath, targetUser.ID, i)
	}
	infoformsMu.Unlock()

	wv(terminal, "\r\n|10All infoforms deleted.\r\n", outputMode)
	log.Printf("INFO: Node %d: SysOp %s nuked infoforms for user %s", nodeNumber, currentUser.Handle, targetUser.Handle)
	return currentUser, "", nil
}
