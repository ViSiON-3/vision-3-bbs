package menu

import (
	"errors"
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/scripting"
)

// v3ScriptRunLimit is the run time limit a v3_script door gives its script.
// The limit is set in minutes; a test swaps this to reach it in milliseconds.
var v3ScriptRunLimit = config.DoorConfig.ScriptRunLimit

// v3ScriptRunError is what a v3_script door returns for runErr, a non-nil
// error from the script engine's Run. A caller hanging up and a script
// reaching its run time limit are normal ends (nil), so the caller isn't
// stopped at an error prompt; anything else is logged and returned.
func v3ScriptRunError(ctx *DoorCtx, runErr error) error {
	switch {
	case errors.Is(runErr, scripting.ErrDisconnect):
		slog.Info("user disconnected from V3 script",
			"node", ctx.NodeNumber, "door", ctx.DoorName)
		return nil
	case errors.Is(runErr, scripting.ErrTimeout):
		slog.Info("V3 script reached its run time limit",
			"node", ctx.NodeNumber, "door", ctx.DoorName, "max_run_minutes", ctx.Config.MaxRunMinutes)
		return nil
	}
	slog.Error("V3 script error",
		"node", ctx.NodeNumber, "door", ctx.DoorName, "error", runErr)
	return runErr
}
