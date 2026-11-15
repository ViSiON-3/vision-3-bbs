package menu

import (
	"slices"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/stringeditor"
)

// The string editor's preview shows placeholders as written. One that begins
// with a pipe code (|PV starts with |P, save-cursor) has to be on its list,
// or the preview would read it as the code and some text.
func TestPreviewKnowsPlaceholdersThatStartLikeCodes(t *testing.T) {
	for _, k := range sessionPlaceholderKeys {
		if _, _, n := ansi.PipeCodeAt([]byte(k), 0); n > 0 && n < len(k) && !slices.Contains(stringeditor.PreviewPlaceholders, k) {
			t.Errorf("%s starts with a pipe code; add it to stringeditor.PreviewPlaceholders", k)
		}
	}
}
