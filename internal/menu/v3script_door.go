//go:build !windows

package menu

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"

	"github.com/stlalpha/vision3/internal/scripting"
	"github.com/stlalpha/vision3/internal/version"
)

// executeV3ScriptDoor runs a Vision/3 VPL script using the embedded JS engine.
func executeV3ScriptDoor(ctx *DoorCtx) error {
	if ctx.Config.Script == "" {
		return fmt.Errorf("v3_script door '%s' has no script configured", ctx.DoorName)
	}

	workDir := ctx.Config.WorkingDirectory
	if workDir == "" {
		workDir = "."
	}
	workDir, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}

	// Set up a read interrupt so the engine's copier goroutine can be
	// cleanly stopped when the script exits, preventing it from consuming
	// the next keypress meant for the menu system.
	readInterrupt := make(chan struct{})
	if ri, ok := ctx.Session.(interface{ SetReadInterrupt(<-chan struct{}) }); ok {
		ri.SetReadInterrupt(readInterrupt)
		defer ri.SetReadInterrupt(nil)
	}

	cfg := scripting.ScriptConfig{
		Script:     ctx.Config.Script,
		WorkingDir: workDir,
		Args:       ctx.Config.Args,
	}

	session := &scripting.SessionContext{
		Session:          ctx.Session,
		OutputMode:       ctx.OutputMode,
		UserID:           ctx.User.ID,
		UserHandle:       ctx.User.Handle,
		UserRealName:     ctx.User.RealName,
		AccessLevel:      ctx.User.AccessLevel,
		TimeLimit:        ctx.User.TimeLimit,
		TimesCalled:      ctx.User.TimesCalled,
		Location:         ctx.User.GroupLocation,
		ScreenWidth:      ctx.User.ScreenWidth,
		ScreenHeight:     ctx.User.ScreenHeight,
		NodeNumber:       ctx.NodeNumber,
		SessionStartTime: ctx.SessionStartTime,
		BoardName:        ctx.Executor.ServerCfg.BoardName,
		SysOpName:        ctx.Executor.ServerCfg.SysOpName,
		BBSVersion:       version.Number,
	}

	// Derive from the SSH session context so scripts cancel on disconnect.
	engineCtx, engineCancel := context.WithCancel(ctx.Session.Context())
	defer engineCancel()

	providers := &scripting.Providers{
		UserMgr:         ctx.UserManager,
		CurrentUser:     ctx.CurrentUser,
		MessageMgr:      ctx.Executor.MessageMgr,
		FileMgr:         ctx.Executor.FileMgr,
		SessionRegistry: ctx.Executor.SessionRegistry,
	}

	eng := scripting.NewEngine(engineCtx, session, cfg, providers)

	log.Printf("INFO: Node %d: Starting V3 script '%s' (script: %s)",
		ctx.NodeNumber, ctx.DoorName, ctx.Config.Script)

	runErr := eng.Run(cfg.Script)

	// Interrupt the copier goroutine's blocked Read() so it exits without
	// consuming the user's next keypress, then close the engine.
	close(readInterrupt)
	eng.Close()

	if runErr != nil {
		if errors.Is(runErr, scripting.ErrDisconnect) {
			log.Printf("INFO: Node %d: User disconnected from V3 script '%s'",
				ctx.NodeNumber, ctx.DoorName)
			return nil
		}
		log.Printf("ERROR: Node %d: V3 script '%s' error: %v",
			ctx.NodeNumber, ctx.DoorName, runErr)
		return runErr
	}

	log.Printf("INFO: Node %d: V3 script '%s' completed normally",
		ctx.NodeNumber, ctx.DoorName)
	return nil
}
