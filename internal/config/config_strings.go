package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/stringformat"
	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// LoadStrings loads the string configuration from a JSON file.
func LoadStrings(configPath string) (StringsConfig, error) { // Return the loaded config directly
	filePath := filepath.Join(configPath, "strings.json")
	slog.Info("loading strings configuration", "path", filePath)
	data, err := os.ReadFile(filePath)
	if err != nil {
		slog.Error("failed to read strings file", "path", filePath, "error", err)
		return StringsConfig{}, fmt.Errorf("failed to read strings file %s: %w", filePath, err)
	}

	var loadedConfig StringsConfig // Load into a local variable
	err = json.Unmarshal(data, &loadedConfig)
	if err != nil {
		slog.Error("failed to parse strings JSON", "path", filePath, "error", err)
		return StringsConfig{}, fmt.Errorf("failed to parse strings JSON from %s: %w", filePath, err)
	}

	applyStringDefaults(&loadedConfig)
	warnFormatMismatches(data, filePath)
	slog.Info("successfully loaded strings configuration")
	return loadedConfig, nil // Return the loaded struct
}

// warnFormatMismatches reports any configured string whose format directives no
// longer match the arguments its call site passes.
//
// It only warns. Sprintf does not fail on a mismatch, it prints %!d(MISSING)
// into the middle of the message, so the sysop needs to be told -- but refusing
// to start the BBS over a cosmetic prompt would be worse than the prompt, and
// silently rewriting their string would discard an edit they meant to make.
// This runs on hot reload too, since that path calls LoadStrings.
func warnFormatMismatches(data []byte, filePath string) {
	values, err := stringValues(data)
	if err != nil {
		return // the caller already surfaced any parse failure
	}
	shipped, err := configtemplates.StringDefaults()
	if err != nil {
		// The embedded template is unreadable, but the compiled-in fallbacks
		// still describe the keys they cover, so validate against those rather
		// than skipping the check entirely.
		slog.Debug("no embedded defaults to validate strings against", "error", err)
	}
	defaults := stringformat.MergeDefaults(StringFallbacks, shipped)
	if len(defaults) == 0 {
		return
	}
	for _, p := range stringformat.Validate(values, StringFallbacks, defaults) {
		slog.Warn("configured string does not match the arguments the BBS passes it; "+
			"it will print a malformed message until corrected",
			"path", filePath, "key", p.Key, "expected", p.Expected, "problem", p.Detail)
	}
}

// stringValues extracts the string-valued entries of a strings.json document.
// Numeric entries such as the defColorN fields are skipped rather than failing
// the whole extraction.
func stringValues(data []byte) (map[string]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(raw))
	for key, msg := range raw {
		var s string
		if err := json.Unmarshal(msg, &s); err == nil {
			out[key] = s
		}
	}
	return out, nil
}

// StringFallbacks maps a strings.json key to the value the runtime substitutes
// when the configured value is empty. It is the single source for these
// defaults: LoadStrings applies them, and the string editor reads the same map
// so it can tell a blank entry that still prints something from one that does
// not.
//
// A key belongs here when it was added after strings.json shipped, so an
// upgraded installation will not have it and the feature would otherwise print
// nothing at all.
var StringFallbacks = map[string]string{
	// Conference and new-user notices whose defaults lived as literals at the
	// call site. Lifting them here changes nothing at runtime and makes them
	// editable rather than invisible.
	"confCurrentConfFormat": "\r\n|07(|15%s|07) [|14%s|07]\r\n",
	"confNoAccessibleConfs": "\r\n|12No accessible conferences.|07\r\n",
	"newUsersClosedStr":     "\r\n|12This BBS is not accepting new users at this time.|07\r\n",

	// Without a fallback, an existing strings.json that predates this key
	// yields an empty value, and the notice is skipped silently: the sysop sees
	// notifySysopNewUser defaulting to true and nothing ever arriving. The
	// setting defaults on precisely so an upgrade needs no edits, so the string
	// it depends on has to as well.
	"newUserSysopPage": "|12New user|07: |15%s|07 just signed up from node %d.",

	// The queued counterpart, for the same reason: an older strings.json must
	// not leave the SYSOPNOTICES step with nothing to render. "just signed up"
	// belongs to the live page only -- a queued notice states the age instead,
	// since it is read whenever the sysop next calls.
	"newUserSysopNotice":   "|12New user|07: |15%s|07 signed up |15%s ago|07 from node %d.",
	"v3netNewAreaNotice":   "|15New |14%s|15 area: |11%s|15. Add?",
	"v3netNewAreaDeclined": "|07You won't be asked about |15%s|07 again. To add it later: |15V3Net|07 menu > |15Area Subscriptions|07, highlight it and press |15Space|07.",

	// The rest of the batch download group. These are written straight to the
	// terminal, so shipping them blank printed nothing where a message belongs
	// -- an empty queue, or a failed save, looked like the command did nothing.
	"batchQueueEmpty":   "\r\n|14Your batch queue is empty.|07\r\n",
	"noFilesTagged":     "\r\n|14No files are tagged for download.|07\r\n",
	"filesResolveError": "\r\n|12None of the tagged files could be found; the queue has been cleared.|07\r\n",
	"fileAreaNotFound":  "\r\n|12File area not found.|07\r\n",
	"saveUserError":     "\r\n|12Error saving your account; please try again.|07\r\n",

	// Batch download queue. These four are formatted, and every call site
	// passes their arguments unconditionally, so an empty value does not print
	// nothing -- fmt renders "%!(EXTRA string=FILENAME.ZIP)" onto the caller's
	// screen. They shipped without defaults, so this is what an install has
	// been showing; see TestFormattedStringsAlwaysHaveADefault.
	"addedToBatchFormat":     "|10Added |15%s|10 to the batch queue.|07\r\n",
	"batchClearedFormat":     "|10Cleared |15%d|10 file(s) from the batch queue.|07\r\n",
	"batchCountFormat":       "|07Batch queue: |15%d|07 file(s).|07\r\n",
	"downloadFinishedFormat": "|10Download complete: |15%d|10 succeeded, |15%d|10 failed.|07\r\n",

	// New user signup outcome (added with autoValidateNewUsers; an upgraded
	// strings.json will not have these, and the ready path would otherwise
	// print nothing at all)
	"newUserAccountReady":      "\r\n|15Your account has been created. |10You can log on now.|07\r\n",
	"newUserPendingReview":     "|08A SysOp will review your account.|07\r\n",
	"matrixAccountCannotLogon": "\r\n|14Account '%s' cannot log on yet |08(level |15%d|08, minimum |15%d|08)|14.\r\n|08A SysOp must raise your access.|07\r\n",

	// Require-email signup gate (requireNewUserEmail). Shipped so an upgraded
	// strings.json without these keys still shows the prompts rather than blank
	// lines, and so they appear in the string editor.
	"newUserEmailPrompt":   "\r\n|15Before you go, please leave the |14SysOp|15 a private message.|07\r\n|07Tell them a bit about yourself and why you'd like access.\r\n|08Your account may not be validated without it.|07\r\n",
	"newUserEmailSubject":  "New user application - %s",
	"newUserEmailRequired": "\r\n|12A message to the SysOp is required to complete your registration.|07\r\n",
	"fileScanDatePrompt":   "\r\n|07File newscan since |08(|15MM/DD/YY|08, |15A|08=all files, |15R|08=reset to last logon|08)|07: |15",
	"newUserLoggingIn":     "\r\n|10Thanks! You're all set - logging you in now...|07\r\n",

	// Message newscan notices (added after the scan strings shipped; keep
	// existing strings.json files working without them)
	"scanInvalidDate":  "\r\n|12Invalid date. Enter MM/DD/YY, MM/DD/YYYY, MM-DD-YY, MM-DD-YYYY, YYYY-MM-DD, MMDDYY or MMDDYYYY.|07\r\n",
	"scanInvalidRange": "\r\n|12Invalid range; range cleared.|07\r\n",
	"scanNoMatches":    "\r\n|07No messages match the scan settings.|07\r\n",

	// File search
	"searchFilesPrompt":    "\r\n|15Enter search text |07(min 3 chars)|15: |07",
	"searchFilesMinChars":  "\r\n|12Search text must be at least 3 characters.|07\r\n",
	"searchNoResults":      "\r\n|14No files found matching your search.|07\r\n",
	"searchResultsHeader":  "\r\n|15Search results for: |11%s|07\r\n|08────────────────────────────────────────────────────────────────|07\r\n",
	"searchResultsSummary": "\r\n|15%d file(s) found.|07\r\n",

	// File info
	"fileInfoPrompt": "\r\n|15Filename to view info|15: |07",
	"fileInfoHeader": "\r\n|08════════════════════════════════════════|07\r\n|15File Information|07\r\n|08════════════════════════════════════════|07",

	// File newscan
	"fileNewscanHeader":   "\r\n|15File Newscan|07\r\n|08────────────────────────────────────────|07\r\n",
	"fileNewscanAreaHdr":  "\r\n|11%s |07(%d new files)|07\r\n",
	"fileNewscanNoNew":    "\r\n|14No new files found since your last login.|07\r\n",
	"fileNewscanComplete": "\r\n|15Newscan complete. |11%d|15 new file(s) found.|07\r\n",

	// File newscan config
	"fileNewscanConfigHeader": "|15File Newscan Configuration|07\r\n|08────────────────────────────────────────|07\r\n|07Tag areas to scan for new files|07\r\n",
	"fileNewscanConfigSaved":  "\r\n|15File newscan config saved. |11%d|15 area(s) tagged.|07\r\n",

	// Sysop file review
	"sysopReviewHeader":  "|15SysOp File Review|07",
	"sysopReviewPrompt":  "\r\n|07[|15C|07]hange Desc  [|15R|07]ename  [|15D|07]elete  [|15M|07]ove  [|15S|07]kip  [|15Q|07]uit: |15",
	"sysopReviewMarked":  "|10File marked as reviewed.|07",
	"sysopReviewNoFiles": "\r\n|14No unreviewed files.|07\r\n",
	"sysopReviewScanAll": "\r\n|07Scan all areas? [|15Y|07/|15N|07]: |15",
	"sysopReviewRenamed": "|10File renamed successfully.|07",

	// Want list
	"wantListPrompt":       "\r\n|15Filename to request|15: |07",
	"wantListReasonPrompt": "\r\n|07Reason |08(optional)|07: |07",
	"wantListSubmitted":    "\r\n|10Request submitted.|07\r\n",
	"wantListEmpty":        "\r\n|14No file requests.|07\r\n",
	"wantListHeader":       "\r\n|15File Want List|07\r\n|08────────────────────────────────────────|07\r\n",
	"wantListCleared":      "\r\n|10Want list cleared.|07\r\n",

	// Column config
	"cfgFileColumnsHeader": "\r\n|15File Listing Columns|07\r\n|08────────────────────────────────────────|07\r\n",
	"cfgFileColumnsToggle": "  |15[%s]|07 %-12s : %s\r\n",
	"cfgFileColumnsSaved":  "\r\n|10Column preferences saved.|07\r\n",

	// Door access control
	"doorAccessDenied": "\r\n|14Access denied to door: |11%s|07\r\n",
	"doorBusyFormat":   "\r\n|14Door is currently in use: |11%s|07\r\n",

	// Remote (rlogin) door notices. Without defaults an upgraded
	// strings.json would connect and disconnect in total silence, leaving
	// the caller staring at a blank screen with no idea a door server is
	// being dialled.
	"doorRemoteConnecting":    "\r\n|15Connecting to |11%s|15, please wait...|07\r\n",
	"doorRemoteConnectFailed": "\r\n|12Unable to connect to |11%s|12. Please try again later.|07\r\n",
	"doorRemoteDisconnected":  "\r\n|08Disconnected from |07%s|08.|07\r\n",

	// PAGESYSOP and split-screen chat. An upgraded strings.json lacks these,
	// and the caller would see blank prompts.
	"pageSysopReasonPrompt": "\r\n|07Reason for paging the SysOp (|15Enter|07 to cancel): ",
	"pageSysopPaging":       "\r\n|11Paging SysOp|07... ",
	"pageSysopUnavailable":  "\r\n|12The SysOp is not available right now.|07\r\n",
	"pageSysopCooldown":     "\r\n|07You paged the SysOp recently. Try again in |15%d|07 minutes.\r\n",
	"sysopChatHeader":       " |15%s|07 chatting with |15%s|07 ",
	"sysopChatBack":         "\r\n|08[back from chat, press Enter]|07",

	// Time limit notices. Without them an upgraded strings.json would warn
	// nobody and disconnect callers with no word of why.
	"timeLimitExpired": "\r\n|12Your time limit for this call is up. Call again soon!|07\r\n",
	"timeLimitWarning": "\r\n|14You have |15%d|14 minute(s) left this call.|07\r\n",

	// The User Konfig form (#429). Its text was hard-coded until these keys
	// existed, so every strings.json written before them lacks them all.
	"konfigTitle":               "User Konfig",
	"konfigLegend":              "|08[|15Q|08/|15ESC|08] |07Done",
	"konfigSectionTerminal":     "Terminal",
	"konfigSectionMessages":     "Messages",
	"konfigSectionPersonal":     "Personal",
	"konfigSectionFiles":        "Files",
	"konfigScreenWidthLabel":    "Screen Width",
	"konfigScreenWidthHelp":     "Columns your terminal shows (40-255). Used for layout and word wrap.",
	"konfigScreenHeightLabel":   "Screen Height",
	"konfigScreenHeightHelp":    "Rows your terminal shows (21-60). Used for paging and full-screen views.",
	"konfigEncodingLabel":       "Encoding",
	"konfigEncodingHelp":        "Auto detects it on every call. Force UTF-8 or CP437 only if Auto is wrong.",
	"konfigHotKeysLabel":        "Hot Keys",
	"konfigHotKeysHelp":         "On: menu commands run the moment you press their key, no Enter needed.",
	"konfigHeaderStyleLabel":    "Header Style",
	"konfigHeaderStyleHelp":     "How message headers look in the reader. Opens the style picker.",
	"konfigAutoSigLabel":        "Auto-Signature",
	"konfigAutoSigHelp":         "Up to 5 lines added to the end of every message you post.",
	"konfigRealNameLabel":       "Real Name",
	"konfigRealNameHelp":        "Used in areas that require real names. First and last name.",
	"konfigLocationLabel":       "Location",
	"konfigLocationHelp":        "Your group or location, shown in user lists and last callers.",
	"konfigUserNoteLabel":       "User Note",
	"konfigUserNoteHelp":        "A short line about you, shown in user lists and last callers.",
	"konfigPasswordLabel":       "Password",
	"konfigPasswordHelp":        "Change your login password. You'll need your current one.",
	"konfigListingModeLabel":    "Listing Mode",
	"konfigListingModeHelp":     "Lightbar: arrow-key file browser. Classic: scrolling text list.",
	"konfigFileColumnsLabel":    "File Columns",
	"konfigFileColumnsHelp":     "Choose which columns the file listing shows.",
	"konfigDefaultFormat":       "%s (default)",
	"konfigNotSet":              "(not set)",
	"konfigOn":                  "On",
	"konfigOff":                 "Off",
	"konfigAutoFormat":          "Auto (%s)",
	"konfigLightbar":            "Lightbar",
	"konfigClassic":             "Classic",
	"konfigNotChosen":           "(not chosen)",
	"konfigStyleFormat":         "Style %d",
	"konfigNone":                "(none)",
	"konfigOneLine":             "1 line",
	"konfigLinesFormat":         "%d lines",
	"konfigAll":                 "All",
	"konfigCountFormat":         "%d of %d",
	"konfigSaveError":           "|12Couldn't save %s. Please try again.|07",
	"konfigSavedFormat":         "|10Saved.|07 %s is now |15%s|07.",
	"konfigClearedFormat":       "|10Saved.|07 %s cleared.",
	"konfigUpdatedFormat":       "|10Saved.|07 %s updated.",
	"konfigRangeError":          "|12%s must be a number from %d to %d.|07",
	"konfigRangeHint":           "|08(%d-%d)",
	"konfigEncodingAuto":        "|10Saved.|07 Encoding is now |15Auto|07, detected on every call.",
	"konfigEncodingSaved":       "|10Saved.|07 Encoding is now |15%s|07, starting with your next login.",
	"konfigEncodingMismatch":    "|12Saved, but this terminal is using %s.|07 |15%s|07 starts next login.",
	"konfigRealNameRequired":    "|12Real name cannot be blank: areas flagged real-name-only fall back to the handle without it.|07",
	"konfigRealNameTooShort":    "|12Real name must be at least %d characters.|07",
	"konfigRealNameNeedsSpace":  "|12Real name must contain a space (first and last name).|07",
	"konfigPwCurrent":           "Current password",
	"konfigPwNew":               "New password",
	"konfigPwAgain":             "Type it again",
	"konfigPwIncorrect":         "|12Incorrect password.|07 Nothing was changed.",
	"konfigPwTooShort":          "|12Passwords must be at least %d characters.|07 Nothing was changed.",
	"konfigPwTooLong":           "|12Passwords can be at most %d bytes.|07 Nothing was changed.",
	"konfigPwMismatch":          "|12The passwords didn't match.|07 Nothing was changed.",
	"konfigPwFailed":            "|12Couldn't change your password. Please try again.|07",
	"konfigPwChanged":           "|10Saved.|07 Your password has been changed.",
	"konfigAutoSigChoice":       "|15Auto-Signature|08: |08[|15E|08]|07dit  |08[|15D|08]|07elete  |08[|15Esc|08]|07 Cancel",
	"konfigAutoSigDeleted":      "|10Saved.|07 Auto-Signature deleted.",
	"konfigAutoSigEditorFailed": "|12The editor couldn't start.|07 Nothing was changed.",
	"konfigAutoSigUnchanged":    "|07Auto-Signature not changed.",
	"konfigAutoSigCleared":      "|10Saved.|07 Auto-Signature cleared.",
	"konfigAutoSigTruncated":    "|10Saved.|07 Auto-Signature kept to its first %d lines.",
	"konfigAutoSigUpdated":      "|10Saved.|07 Auto-Signature updated.",
	"konfigColumnsTitle":        "File Columns",
	"konfigColumnsHint":         "Enter switches   Esc when done",
	"konfigColumnsKeepOne":      "|12At least one column has to stay on.|07",
	"konfigColumnSaved":         "|10Saved.|07 %s column is now |15%s|07.",
	"konfigColumnName":          "Name",
	"konfigColumnSize":          "Size",
	"konfigColumnDate":          "Date",
	"konfigColumnDownloads":     "Downloads",
	"konfigColumnUploader":      "Uploader",
	"konfigColumnDescription":   "Description",
}

// applyStringDefaults fills in StringFallbacks values for any string field that
// is empty, matching each map key against the struct's json tags.
func applyStringDefaults(c *StringsConfig) {
	v := reflect.ValueOf(c).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() != reflect.String || !field.CanSet() || field.String() != "" {
			continue
		}
		key, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if def, ok := StringFallbacks[key]; ok {
			field.SetString(def)
		}
	}
}
