package menu

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

func (e *MenuExecutor) resolveCurrentAreaTokens(currentUser *user.User, currentAreaName string) (string, string) {
	areaTag := "None"
	areaName := strings.TrimSpace(currentAreaName)

	if currentUser == nil {
		if areaName == "" {
			areaName = "None"
		}
		return areaTag, areaName
	}

	if currentUser.CurrentMessageAreaTag != "" {
		areaTag = currentUser.CurrentMessageAreaTag
	}
	if e.MessageMgr != nil && currentUser.CurrentMessageAreaID > 0 {
		if area, found := e.MessageMgr.GetAreaByID(currentUser.CurrentMessageAreaID); found {
			if strings.TrimSpace(area.Tag) != "" {
				areaTag = area.Tag
			}
			if strings.TrimSpace(area.Name) != "" {
				areaName = area.Name
			}
		}
	}

	if areaName == "" {
		areaName = "None"
	}
	return areaTag, areaName
}

// resolveFileConferencePath returns "Conference > File Area Name" as plain text.
// Colors should be applied in the template surrounding the placeholder.
func (e *MenuExecutor) resolveFileConferencePath(currentUser *user.User) string {
	confName := "Local"
	areaName := "None"
	if currentUser == nil || e.FileMgr == nil {
		return confName + " > " + areaName
	}
	if currentUser.CurrentFileAreaID > 0 {
		if area, found := e.FileMgr.GetAreaByID(currentUser.CurrentFileAreaID); found {
			if strings.TrimSpace(area.Name) != "" {
				areaName = area.Name
			}
			if area.ConferenceID != 0 && e.ConferenceMgr != nil {
				if conf, found := e.ConferenceMgr.GetByID(area.ConferenceID); found && strings.TrimSpace(conf.Name) != "" {
					confName = conf.Name
				}
			}
		}
	}
	return confName + " > " + areaName
}

// resolveCurrentFileAreaTokens returns the tag and display name for the user's
// current file area. If no file area is set or the area cannot be found, it
// returns "None" for both values.
func (e *MenuExecutor) resolveCurrentFileAreaTokens(currentUser *user.User) (string, string) {
	areaTag := "None"
	areaName := "None"

	if currentUser == nil {
		return areaTag, areaName
	}

	if currentUser.CurrentFileAreaTag != "" {
		areaTag = currentUser.CurrentFileAreaTag
	}
	if e.FileMgr != nil && currentUser.CurrentFileAreaID > 0 {
		if area, found := e.FileMgr.GetAreaByID(currentUser.CurrentFileAreaID); found {
			if strings.TrimSpace(area.Tag) != "" {
				areaTag = area.Tag
			}
			if strings.TrimSpace(area.Name) != "" {
				areaName = area.Name
			}
		}
	}

	return areaTag, areaName
}

// applyCommonTemplateTokens replaces pipe-style tokens (|CFAN, |CFA, |CAN, |CA,
// |UH, |NODE, |DATE, |TIME, etc.) in template bytes before ANSI pipe-code
// processing.  This mirrors the substitution that displayMenuPrompt performs so
// templates behave consistently with prompts.  Longer tokens are replaced first
// to avoid prefix collisions (e.g. |CFAN before |CFA, |CAN before |CA).
func (e *MenuExecutor) applyCommonTemplateTokens(data []byte, currentUser *user.User, nodeNumber int) []byte {
	now := config.NowIn(e.ServerCfg.Timezone)
	fileAreaTag, fileAreaName := e.resolveCurrentFileAreaTokens(currentUser)
	msgAreaTag, msgAreaName := e.resolveCurrentAreaTokens(currentUser, "")

	tokens := map[string]string{
		"|NODE":      strconv.Itoa(nodeNumber),
		"|DATE":      now.Format("01/02/06"),
		"|TIME":      now.Format("3:04 pm"),
		"|CA":        msgAreaTag,
		"|CAN":       msgAreaName,
		"|CFA":       fileAreaTag,
		"|CFAN":      fileAreaName,
		"|FCONFPATH": e.resolveFileConferencePath(currentUser),
		"|UH":        "Guest",
		"|ALIAS":     "Guest",
		"|HANDLE":    "Guest",
		"|LEVEL":     "0",
		"|CC":        "None",
		"|CCN":       "None",
		"|FC":        "None",
		"|FCN":       "None",
	}
	if currentUser != nil {
		tokens["|UH"] = currentUser.Handle
		tokens["|ALIAS"] = currentUser.Handle
		tokens["|HANDLE"] = currentUser.Handle
		tokens["|LEVEL"] = strconv.Itoa(currentUser.AccessLevel)
		if currentUser.CurrentMsgConferenceTag != "" {
			tokens["|CC"] = currentUser.CurrentMsgConferenceTag
		}
		if currentUser.CurrentFileConferenceTag != "" {
			tokens["|FC"] = currentUser.CurrentFileConferenceTag
		}
		if e.ConferenceMgr != nil {
			if currentUser.CurrentMsgConferenceID != 0 {
				if conf, ok := e.ConferenceMgr.GetByID(currentUser.CurrentMsgConferenceID); ok {
					tokens["|CCN"] = conf.Name
					if tokens["|CC"] == "None" {
						tokens["|CC"] = conf.Tag
					}
				}
			}
			if currentUser.CurrentFileConferenceID != 0 {
				if conf, ok := e.ConferenceMgr.GetByID(currentUser.CurrentFileConferenceID); ok {
					tokens["|FCN"] = conf.Name
					if tokens["|FC"] == "None" {
						tokens["|FC"] = conf.Tag
					}
				}
			}
		}
	}

	// Sort longest-key-first so |CFAN is replaced before |CFA, etc.
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	pairs := make([]string, 0, len(tokens)*2)
	for _, k := range keys {
		pairs = append(pairs, k, tokens[k])
	}
	return []byte(strings.NewReplacer(pairs...).Replace(string(data)))
}

// displayFile reads and displays an ANSI file from the MENU SET's ansi directory.
// If clearFirst is true, prepends the ANSI clear sequence so clear and content go out in one write.
func (e *MenuExecutor) displayFile(terminal *term.Terminal, filename string, outputMode ansi.OutputMode, clearFirst ...bool) error {
	// Construct full path using MenuSetPath
	filePath := filepath.Join(e.MenuSetPath, "ansi", filename)

	// Read ANSI content via helper (strips SAUCE metadata)
	data, err := ansi.GetAnsiFileContent(filePath)
	if err != nil {
		slog.Error("failed to read ANSI file", "path", filePath, "error", err)
		errMsg := fmt.Sprintf(e.LoadedStrings.ExecFileLoadError, filename)
		writeErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		if writeErr != nil {
			slog.Error("failed writing displayFile error message", "error", writeErr)
			return fmt.Errorf("read: %w; write error: %w", err, writeErr)
		}
		return err
	}
	if len(clearFirst) > 0 && clearFirst[0] {
		data = append([]byte(ansi.ClearScreen()), data...)
	}

	// Expand AT-codes before pipe code processing.
	// Use level 1 (default MinLevel) since displayFile lacks user context.
	data = expandRandomRumorATCode(data, e.RootConfigPath, 1)

	// Process pipe codes before output — ANSI escape sequences produced are
	// ASCII-safe and work correctly in both CP437 and UTF-8 output modes.
	data = ansi.ReplacePipeCodes(data)

	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	var writeErr error
	if outputMode == ansi.OutputModeCP437 {
		_, writeErr = terminal.Write(data)
	} else {
		writeErr = terminalio.WriteProcessedBytes(terminal, data, outputMode)
	}
	if writeErr != nil {
		slog.Error("failed to write ANSI file", "path", filePath, "error", writeErr)
		return writeErr
	}

	return nil
}

// deliverPendingPages checks for and displays any queued page messages.
func (e *MenuExecutor) deliverPendingPages(terminal *term.Terminal, nodeNumber int, outputMode ansi.OutputMode) {
	if e.SessionRegistry == nil {
		return
	}
	sess := e.SessionRegistry.Get(nodeNumber)
	if sess == nil {
		return
	}
	pages := sess.DrainPages()
	for _, page := range pages {
		if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+page+"\r\n")), outputMode); err != nil {
			slog.Error("failed to deliver page", "node", nodeNumber, "error", err)
			return
		}
	}
}

// displayPrompt handles rendering the menu prompt, including file includes and placeholder substitution.
// Added currentAreaName parameter
func (e *MenuExecutor) displayPrompt(terminal *term.Terminal, menu *MenuRecord, currentUser *user.User, userManager *user.UserMgr, nodeNumber int, currentMenuName string, sessionStartTime time.Time, outputMode ansi.OutputMode, currentAreaName string) error {
	promptParts := make([]string, 0, 2)
	if strings.TrimSpace(menu.Prompt1) != "" {
		promptParts = append(promptParts, menu.Prompt1)
	}
	if strings.TrimSpace(menu.Prompt2) != "" {
		promptParts = append(promptParts, menu.Prompt2)
	}

	if currentMenuName == "MAIN" {
		isAdmin := currentUser != nil && currentUser.AccessLevel >= 100
		pendingCount := pendingValidationCount(userManager)
		showValidationLine := isAdmin && pendingCount > 0
		if !showValidationLine {
			filtered := make([]string, 0, len(promptParts))
			for _, part := range promptParts {
				if strings.Contains(part, "|PV") {
					continue
				}
				filtered = append(filtered, part)
			}
			promptParts = filtered
		}
	}

	promptString := strings.Join(promptParts, "\r\n")

	if promptString == "" {
		if e.LoadedStrings.DefPrompt != "" { // Use loaded strings
			promptString = e.LoadedStrings.DefPrompt
		} else {
			slog.Warn("default prompt empty and menu prompt fields empty, no prompt will be displayed", "menu", currentMenuName)
			return nil // Explicitly return nil if no prompt string can be determined
		}
	}

	slog.Debug("displaying menu prompt", "menu", currentMenuName)

	newUsersStatus := "NO"
	if e.GetServerConfig().AllowNewUsers {
		newUsersStatus = "YES"
	}

	now := config.NowIn(e.ServerCfg.Timezone)
	currentAreaTag, currentAreaDisplayName := e.resolveCurrentAreaTokens(currentUser, currentAreaName)
	currentFileAreaTag, currentFileAreaDisplayName := e.resolveCurrentFileAreaTokens(currentUser)

	placeholders := map[string]string{
		"|NODE":     strconv.Itoa(nodeNumber), // Node Number
		"|DATE":     now.Format("01/02/06"),
		"|TIME":     now.Format("3:04 pm"),
		"|MN":       currentMenuName,            // Menu Name
		"|PV":       "0",                        // Pending validations
		"|UH":       "Guest",                    // User Handle
		"|NEWUSERS": newUsersStatus,             // Allow new users (YES/NO)
		"|ALIAS":    "Guest",                    // Default
		"|HANDLE":   "Guest",                    // Default
		"|LEVEL":    "0",                        // Default
		"|NAME":     "Guest User",               // Default
		"|GL":       "",                         // Group/Location default
		"|UN":       "",                         // User note (privateNote) default
		"|UPLDS":    "0",                        // Default
		"|DNLDS":    "0",                        // Default
		"|POSTS":    "0",                        // Default
		"|CALLS":    "0",                        // Default
		"|LCALL":    "Never",                    // Default
		"|TL":       "N/A",                      // Default
		"|CA":       currentAreaTag,             // Current message area tag
		"|CAN":      currentAreaDisplayName,     // Current message area display name
		"|CFA":      currentFileAreaTag,         // Current file area tag
		"|CFAN":     currentFileAreaDisplayName, // Current file area display name
		"|CC":       "None",                     // Current message conference tag default
		"|CCN":      "None",                     // Current message conference name default
		"|FC":       "None",                     // Current file conference tag default
		"|FCN":      "None",                     // Current file conference name default
	}

	// Populate user-specific placeholders if logged in
	if currentUser != nil {
		placeholders["|UH"] = currentUser.Handle
		placeholders["|ALIAS"] = currentUser.Handle
		placeholders["|HANDLE"] = currentUser.Handle
		placeholders["|LEVEL"] = strconv.Itoa(currentUser.AccessLevel)
		placeholders["|NAME"] = currentUser.RealName
		placeholders["|GL"] = currentUser.GroupLocation
		placeholders["|UN"] = currentUser.PrivateNote
		placeholders["|UPLDS"] = strconv.Itoa(currentUser.NumUploads)
		placeholders["|CALLS"] = strconv.Itoa(currentUser.TimesCalled)
		if !currentUser.LastLogin.IsZero() {
			placeholders["|LCALL"] = currentUser.LastLogin.Format("01/02/06")
		}

		// Set |CC/|CCN based on user's current message conference
		if currentUser.CurrentMsgConferenceTag != "" {
			placeholders["|CC"] = currentUser.CurrentMsgConferenceTag
		}
		if e.ConferenceMgr != nil && currentUser.CurrentMsgConferenceID != 0 {
			if conf, ok := e.ConferenceMgr.GetByID(currentUser.CurrentMsgConferenceID); ok {
				placeholders["|CCN"] = conf.Name
			}
		}

		// Set |FC/|FCN based on user's current file conference
		if currentUser.CurrentFileConferenceTag != "" {
			placeholders["|FC"] = currentUser.CurrentFileConferenceTag
		}
		if e.ConferenceMgr != nil && currentUser.CurrentFileConferenceID != 0 {
			if conf, ok := e.ConferenceMgr.GetByID(currentUser.CurrentFileConferenceID); ok {
				placeholders["|FCN"] = conf.Name
			}
		}

		// Calculate Time Left |TL
		if currentUser.TimeLimit <= 0 {
			placeholders["|TL"] = "Unlimited"
		} else {
			elapsedSeconds := time.Since(sessionStartTime).Seconds()
			totalSeconds := float64(currentUser.TimeLimit * 60)
			remainingSeconds := totalSeconds - elapsedSeconds
			if remainingSeconds < 0 {
				remainingSeconds = 0
			}
			remainingMinutes := int(remainingSeconds / 60)
			placeholders["|TL"] = strconv.Itoa(remainingMinutes)
		}

		if currentMenuName == "MAIN" && currentUser.AccessLevel >= 100 {
			placeholders["|PV"] = strconv.Itoa(pendingValidationCount(userManager))
		}
	} // End if currentUser != nil

	// Replace longer placeholders before shorter ones to avoid prefix collisions (e.g. |CAN vs |CA).
	replacementPairs := make([]string, 0, len(placeholders)*2)
	orderedKeys := make([]string, 0, len(placeholders))
	for key := range placeholders {
		orderedKeys = append(orderedKeys, key)
	}
	sort.SliceStable(orderedKeys, func(i, j int) bool {
		return len(orderedKeys[i]) > len(orderedKeys[j])
	})
	for _, key := range orderedKeys {
		replacementPairs = append(replacementPairs, key, placeholders[key])
	}
	substitutedPrompt := strings.NewReplacer(replacementPairs...).Replace(promptString)

	// Replace @CODE@ AT-codes with width support (@UC@, @UC:5@, @UC##@, @U@, etc.)
	promptBytes := replaceMenuATCode([]byte(substitutedPrompt), "UC", strconv.Itoa(userManager.GetUserCount()))
	promptBytes = replaceMenuATCode(promptBytes, "U", strconv.Itoa(e.SessionRegistry.ActiveCount()))
	substitutedPrompt = string(promptBytes)

	processedPrompt, err := e.processFileIncludes(substitutedPrompt, 0) // Pass 'e'
	if err != nil {
		slog.Error("failed processing file includes in prompt", "menu", currentMenuName, "error", err)

		// Use RootAssetsPath for global assets if needed, or MenuSetPath for set-specific
		// pausePrompt := e.LoadedStrings.PauseString // This comes from global strings
		// ... (rest of pause logic) ...
		return err // Use original error if includes fail
	}

	// 2b. Expand @RR@ after file includes so %%file.ans%% content is also processed.
	rumorLevel := 1 // default MinLevel when no user context
	if currentUser != nil {
		rumorLevel = currentUser.AccessLevel
	}
	processedPromptBytes := expandRandomRumorATCode([]byte(processedPrompt), e.RootConfigPath, rumorLevel)

	// 3. Process pipe codes in the final string (includes/placeholders already processed)
	rawPromptBytes := ansi.ReplacePipeCodes(processedPromptBytes)

	// 4. Process character encoding based on outputMode (Reverted to manual loop)
	var finalBuf bytes.Buffer
	finalBuf.Write([]byte("\r\n")) // Add newline prefix

	for i := 0; i < len(rawPromptBytes); i++ {
		b := rawPromptBytes[i]
		if b < 128 || outputMode == ansi.OutputModeCP437 {
			// ASCII or CP437 mode, write raw byte
			finalBuf.WriteByte(b)
		} else {
			// UTF-8 mode, convert extended characters
			r := ansi.Cp437ToUnicode[b] // Use the exported map
			if r == 0 && b != 0 {
				finalBuf.WriteByte('?') // Fallback
			} else if r != 0 {
				finalBuf.WriteRune(r)
			}
		}
	}

	// 5. Write the final processed bytes using the terminal's standard Write (Reverted)
	err = terminalio.WriteProcessedBytes(terminal, finalBuf.Bytes(), outputMode)
	if err != nil {
		slog.Error("failed writing processed prompt", "menu", currentMenuName, "error", err)
		return err
	}

	return nil
}

// includeTagRe matches %%filename.ext%% include tags. Package-level so menu
// renders don't recompile it on every call and recursion level.
var includeTagRe = regexp.MustCompile(`%%[a-zA-Z0-9_\-]+\.[a-zA-Z0-9]+%%`)

// processFileIncludes recursively replaces %%filename.ans tags with file content.
// It now looks for included files within the MENU SET's ansi directory.
func (e *MenuExecutor) processFileIncludes(prompt string, depth int) (string, error) {
	const maxDepth = 5 // Limit recursion depth
	if depth > maxDepth {
		slog.Warn("exceeded maximum file inclusion depth, stopping processing", "maxDepth", maxDepth)
		return prompt, nil
	}

	processedAny := false
	result := includeTagRe.ReplaceAllStringFunc(prompt, func(match string) string {
		processedAny = true
		// match is "%%name.ext%%"; strip the delimiters instead of re-matching.
		fileName := strings.TrimSuffix(strings.TrimPrefix(match, "%%"), "%%")
		// Look for included file in MenuSetPath/ansi
		filePath := filepath.Join(e.MenuSetPath, "ansi", fileName)

		slog.Debug("including file in prompt", "path", filePath, "depth", depth)
		data, err := os.ReadFile(filePath)
		if err != nil {
			slog.Warn("failed to read included file, skipping inclusion", "path", filePath, "error", err)
			return ""
		}
		return string(data)
	})

	if processedAny {
		return e.processFileIncludes(result, depth+1)
	}

	return result, nil
}
