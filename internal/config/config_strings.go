package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// applyStringDefaults fills in default values for any string keys that are empty.
func applyStringDefaults(c *StringsConfig) {
	d := func(field *string, val string) {
		if *field == "" {
			*field = val
		}
	}

	// Message newscan notices (added after the scan strings shipped; keep
	// existing strings.json files working without them)
	d(&c.ScanInvalidDate, "\r\n|12Invalid date. Enter MM/DD/YY, MM/DD/YYYY, MM-DD-YY, MM-DD-YYYY, YYYY-MM-DD, MMDDYY or MMDDYYYY.|07\r\n")
	d(&c.ScanInvalidRange, "\r\n|12Invalid range; range cleared.|07\r\n")
	d(&c.ScanNoMatches, "\r\n|07No messages match the scan settings.|07\r\n")

	// File search
	d(&c.SearchFilesPrompt, "\r\n|15Enter search text |07(min 3 chars)|15: |07")
	d(&c.SearchFilesMinChars, "\r\n|12Search text must be at least 3 characters.|07\r\n")
	d(&c.SearchNoResults, "\r\n|14No files found matching your search.|07\r\n")
	d(&c.SearchResultsHeader, "\r\n|15Search results for: |11%s|07\r\n|08────────────────────────────────────────────────────────────────|07\r\n")
	d(&c.SearchResultsSummary, "\r\n|15%d file(s) found.|07\r\n")

	// File info
	d(&c.FileInfoPrompt, "\r\n|15Filename to view info|15: |07")
	d(&c.FileInfoHeader, "\r\n|08════════════════════════════════════════|07\r\n|15File Information|07\r\n|08════════════════════════════════════════|07")

	// File newscan
	d(&c.FileNewscanHeader, "\r\n|15File Newscan|07\r\n|08────────────────────────────────────────|07\r\n")
	d(&c.FileNewscanAreaHdr, "\r\n|11%s |07(%d new files)|07\r\n")
	d(&c.FileNewscanNoNew, "\r\n|14No new files found since your last login.|07\r\n")
	d(&c.FileNewscanComplete, "\r\n|15Newscan complete. |11%d|15 new file(s) found.|07\r\n")

	// File newscan config
	d(&c.FileNewscanConfigHeader, "|15File Newscan Configuration|07\r\n|08────────────────────────────────────────|07\r\n|07Tag areas to scan for new files|07\r\n")
	d(&c.FileNewscanConfigSaved, "\r\n|15File newscan config saved. |11%d|15 area(s) tagged.|07\r\n")

	// Sysop file review
	d(&c.SysopReviewHeader, "|15SysOp File Review|07")
	d(&c.SysopReviewPrompt, "\r\n|07[|15C|07]hange Desc  [|15R|07]ename  [|15D|07]elete  [|15M|07]ove  [|15S|07]kip  [|15Q|07]uit: |15")
	d(&c.SysopReviewMarked, "|10File marked as reviewed.|07")
	d(&c.SysopReviewNoFiles, "\r\n|14No unreviewed files.|07\r\n")
	d(&c.SysopReviewScanAll, "\r\n|07Scan all areas? [|15Y|07/|15N|07]: |15")
	d(&c.SysopReviewRenamed, "|10File renamed successfully.|07")

	// Want list
	d(&c.WantListPrompt, "\r\n|15Filename to request|15: |07")
	d(&c.WantListReasonPrompt, "\r\n|07Reason |08(optional)|07: |07")
	d(&c.WantListSubmitted, "\r\n|10Request submitted.|07\r\n")
	d(&c.WantListEmpty, "\r\n|14No file requests.|07\r\n")
	d(&c.WantListHeader, "\r\n|15File Want List|07\r\n|08────────────────────────────────────────|07\r\n")
	d(&c.WantListCleared, "\r\n|10Want list cleared.|07\r\n")

	// Column config
	d(&c.CfgFileColumnsHeader, "\r\n|15File Listing Columns|07\r\n|08────────────────────────────────────────|07\r\n")
	d(&c.CfgFileColumnsToggle, "  |15[%s]|07 %-12s : %s\r\n")
	d(&c.CfgFileColumnsSaved, "\r\n|10Column preferences saved.|07\r\n")

	// Door access control
	d(&c.DoorAccessDenied, "\r\n|14Access denied to door: |11%s|07\r\n")
	d(&c.DoorBusyFormat, "\r\n|14Door is currently in use: |11%s|07\r\n")
}
