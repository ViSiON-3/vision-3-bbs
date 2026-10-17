package stringformat

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/stringhook"
)

// A key that cannot run a hook has a !door value checked as written, so the
// missing directives are reported instead of the value being accepted.
func TestNoHookKeyValueCheckedAsWritten(t *testing.T) {
	var key string
	for k := range stringhook.NoHookKeys {
		if IsFormatted(k) {
			key = k
			break
		}
	}
	if key == "" {
		t.Skip("no formatted key is listed in NoHookKeys")
	}
	if err := ValidateValue(key, "!door", "", "%s"); err == nil {
		t.Errorf("ValidateValue(%s, !door) = nil, want a directive mismatch", key)
	}
}
