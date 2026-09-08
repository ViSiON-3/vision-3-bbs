package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	slog.Info("successfully loaded strings configuration")
	return loadedConfig, nil // Return the loaded struct
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
	// New user signup outcome (added with autoValidateNewUsers; an upgraded
	// strings.json will not have these, and the ready path would otherwise
	// print nothing at all)
	"newUserAccountReady":      "\r\n|15Your account has been created. |10You can log on now.|07\r\n",
	"newUserPendingReview":     "|08A SysOp will review your account.|07\r\n",
	"matrixAccountCannotLogon": "\r\n|14Account '%s' cannot log on yet |08(level |15%d|08, minimum |15%d|08)|14.\r\n|08A SysOp must raise your access.|07\r\n",

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
