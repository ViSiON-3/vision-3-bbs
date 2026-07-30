package menu

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// adminTruncate's ellipsis reaches the terminal through WriteProcessedBytes,
// which maps every rune to a CP437 byte in CP437 mode and substitutes '?' for
// anything CP437 lacks. U+2026 ("…") is not in CP437, so a truncated admin
// field must not rely on it — a stray '?' reads as corrupted data.
func TestAdminTruncateEllipsisSurvivesCP437(t *testing.T) {
	truncated := adminTruncate(strings.Repeat("A", 40), 10)

	var buf bytes.Buffer
	if err := terminalio.WriteProcessedBytes(&buf, []byte(truncated), ansi.OutputModeCP437); err != nil {
		t.Fatalf("WriteProcessedBytes: %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "?") {
		t.Errorf("CP437 render of %q contains '?': %q — the ellipsis has no CP437 mapping", truncated, got)
	}
	if len(got) != 10 {
		t.Errorf("CP437 render = %d bytes (%q), want 10 columns", len(got), got)
	}
}
