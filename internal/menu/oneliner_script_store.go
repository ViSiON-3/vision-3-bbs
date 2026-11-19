package menu

import (
	"errors"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/scripting"
)

// errEmptyOneliner is returned when there is nothing to post after trimming.
var errEmptyOneliner = errors.New("oneliner is empty")

// onelinerScriptStore adapts the oneliner file store to the scripting
// engine's OnelinerStore interface, so a VPL door can read recent
// oneliners and append one. It shares the file, format, length cap and
// mutex with the built-in ONELINERS command.
type onelinerScriptStore struct {
	e *MenuExecutor
}

// Recent returns the newest oneliners first, up to limit (0 or less = all).
func (o onelinerScriptStore) Recent(limit int) []scripting.OnelinerView {
	records, err := loadOnelinerRecords(onelinerFilePath(o.e.dataDir()))
	if err != nil {
		return nil
	}
	out := make([]scripting.OnelinerView, 0, len(records))
	n := 0
	for i := len(records) - 1; i >= 0; i-- {
		if limit > 0 && n == limit {
			break
		}
		r := records[i]
		handle := strings.TrimSpace(r.PostedByHandle)
		if handle == "" {
			handle = strings.TrimSpace(r.PostedByUsername)
		}
		var posted time.Time
		if r.PostedAt != "" {
			if t, perr := time.Parse(time.RFC3339, r.PostedAt); perr == nil {
				posted = t
			}
		}
		out = append(out, scripting.OnelinerView{
			Text:      r.Text,
			Handle:    handle,
			Anonymous: r.Anonymous,
			PostedAt:  posted,
		})
		n++
	}
	return out
}

// Add appends a oneliner posted as the given caller. Empty lines are
// rejected; the text is capped the same way the built-in command caps it.
func (o onelinerScriptStore) Add(handle, username, text string, anonymous bool) error {
	text = truncateOnelinerPreservePipeCodes(text, oneLinerMaxLength)
	if strings.TrimSpace(text) == "" {
		return errEmptyOneliner
	}
	if strings.TrimSpace(handle) == "" {
		handle = "Unknown"
	}
	if strings.TrimSpace(username) == "" {
		username = handle
	}
	path := onelinerFilePath(o.e.dataDir())
	onelinerMutex.Lock()
	defer onelinerMutex.Unlock()
	records, err := loadOnelinerRecords(path)
	if err != nil {
		records = nil
	}
	records = append(records, onelinerRecord{
		Text:             text,
		Anonymous:        anonymous,
		PostedByUsername: username,
		PostedByHandle:   handle,
		PostedAt:         time.Now().UTC().Format(time.RFC3339),
	})
	return saveOnelinerRecords(path, records)
}
