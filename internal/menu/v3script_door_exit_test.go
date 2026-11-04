package menu

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/scripting"
)

// TestV3ScriptRunError checks which engine errors end a v3_script door
// normally: a hang-up and the run time limit, wrapped or not.
func TestV3ScriptRunError(t *testing.T) {
	ctx := &DoorCtx{DoorName: "READER"}
	scriptErr := errors.New("ReferenceError: x is not defined")
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"disconnect", scripting.ErrDisconnect, nil},
		{"run time limit", scripting.ErrTimeout, nil},
		{"wrapped run time limit", fmt.Errorf("run: %w", scripting.ErrTimeout), nil},
		{"script error", scriptErr, scriptErr},
	} {
		if got := v3ScriptRunError(ctx, tc.err); got != tc.want {
			t.Errorf("%s: v3ScriptRunError = %v, want %v", tc.name, got, tc.want)
		}
	}
}
