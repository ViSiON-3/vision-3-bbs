package configeditor

import (
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// Field definitions for the Logging sub-screen, plus the log-rolling type
// lookup it presents.

// logRollingTypes is the single source of truth for the Rolling Type lookup:
// the persisted LoggingConfig.Type, the short label shown in the form, and the
// picker description. logTypeToLabel / logLabelToType convert against it.
var logRollingTypes = []struct {
	typ     int
	label   string
	display string
}{
	{config.LogTypeNone, "None", "None - single file, no rotation"},
	{config.LogTypeSize, "Size", "Size - rotate at Max Size KB, keep Max Files"},
	{config.LogTypeDaily, "Daily", "Daily - new file per day, keep Max Files days"},
}

// logTypeToLabel maps a persisted type to its label, falling back to "None" for
// an unmapped/hand-edited value (the rolling writer treats unknown types as
// no-rotation, so "None" is the consistent display).
func logTypeToLabel(typ int) string {
	for _, t := range logRollingTypes {
		if t.typ == typ {
			return t.label
		}
	}
	return "None"
}

func logLabelToType(label string) int {
	for _, t := range logRollingTypes {
		if t.label == label {
			return t.typ
		}
	}
	return config.LogTypeNone
}

func logRollingTypeItems() []LookupItem {
	items := make([]LookupItem, len(logRollingTypes))
	for i, t := range logRollingTypes {
		items[i] = LookupItem{Value: t.label, Display: t.display}
	}
	return items
}

// sysFieldsLogging returns fields for the Logging sub-screen.
func sysFieldsLogging(cfg *config.ServerConfig) []fieldDef {
	lg := &cfg.Logging
	return []fieldDef{
		{
			Label: "Log Directory", Help: "Directory for log files (absolute, or relative to BBS root)", Type: ftString, Col: 3, Row: 1, Width: 40,
			Get: func() string { return lg.Dir },
			Set: func(val string) error { lg.Dir = val; return nil },
		},
		{
			Label: "Min Level", Help: "Minimum level written to the log", Type: ftLookup, Col: 3, Row: 2, Width: 8,
			Get: func() string { return lg.Level },
			Set: func(val string) error { lg.Level = val; return nil },
			LookupItems: func() []LookupItem {
				return []LookupItem{
					{Value: "DEBUG", Display: "DEBUG - most verbose"},
					{Value: "INFO", Display: "INFO - normal operation"},
					{Value: "WARN", Display: "WARN - warnings and errors"},
					{Value: "ERROR", Display: "ERROR - errors only"},
				}
			},
		},
		{
			Label: "Rolling Type", Help: "How log files are rotated", Type: ftLookup, Col: 3, Row: 3, Width: 8,
			Get:         func() string { return logTypeToLabel(lg.Type) },
			Set:         func(val string) error { lg.Type = logLabelToType(val); return nil },
			LookupItems: logRollingTypeItems,
		},
		{
			Label: "Cache Writes", Help: "Buffer writes in an 8KB cache (flushed on errors/exit)", Type: ftYesNo, Col: 3, Row: 4, Width: 1,
			Get: func() string { return uitext.BoolToYN(lg.Cache) },
			Set: func(val string) error { lg.Cache = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Max Files", Help: "Retained backups (Size type) or days (Daily type)", Type: ftInteger, Col: 3, Row: 5, Width: 5, Min: 1, Max: 9999,
			Get: func() string { return strconv.Itoa(lg.MaxFiles) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				lg.MaxFiles = n
				return nil
			},
		},
		{
			Label: "Max Size KB", Help: "Rotation threshold in KB (Size type only)", Type: ftInteger, Col: 3, Row: 6, Width: 7, Min: 1, Max: 1048576,
			Get: func() string { return strconv.Itoa(lg.MaxSizeKB) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				lg.MaxSizeKB = n
				return nil
			},
		},
	}
}
