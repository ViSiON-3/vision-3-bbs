//go:build !windows

package menu

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/stlalpha/vision3/internal/syncjs"
)

// executeSyncJSDoor runs a Synchronet-compatible JavaScript door game
// using an embedded JS runtime with the Synchronet API surface.
func executeSyncJSDoor(ctx *DoorCtx) error {
	if ctx.Config.Script == "" {
		return fmt.Errorf("synchronet_js door '%s' has no script configured", ctx.DoorName)
	}

	workDir := ctx.Config.WorkingDirectory
	if workDir == "" {
		workDir = "."
	}
	workDir, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}

	// Create per-node temp directory for system.node_dir
	nodeDir := filepath.Join(os.TempDir(), fmt.Sprintf("vision3_syncjs_node%d", ctx.NodeNumber))
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		return fmt.Errorf("creating node directory: %w", err)
	}
	defer os.RemoveAll(nodeDir)

	// Determine exec_dir — explicit config, or derive from first library path parent
	execDir := ctx.Config.ExecDir
	if execDir == "" {
		execDir = workDir
	}

	cfg := syncjs.SyncJSDoorConfig{
		Script:       ctx.Config.Script,
		WorkingDir:   workDir,
		LibraryPaths: ctx.Config.LibraryPaths,
		Args:         ctx.Config.Args,
		ExecDir:      execDir,
		DataDir:      workDir,
		NodeDir:      nodeDir,
	}

	// Build session context from DoorCtx — bridges menu types to syncjs types
	session := &syncjs.SessionContext{
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
	}

	engineCtx, engineCancel := context.WithCancel(context.Background())
	defer engineCancel()

	eng := syncjs.NewEngine(engineCtx, session, cfg)
	defer eng.Close()

	log.Printf("INFO: Node %d: Starting Synchronet JS door '%s' (script: %s)",
		ctx.NodeNumber, ctx.DoorName, ctx.Config.Script)

	runErr := eng.Run(cfg.Script)

	if runErr != nil {
		if runErr == syncjs.ErrDisconnect {
			log.Printf("INFO: Node %d: User disconnected from JS door '%s'",
				ctx.NodeNumber, ctx.DoorName)
			return nil
		}
		log.Printf("ERROR: Node %d: JS door '%s' error: %v",
			ctx.NodeNumber, ctx.DoorName, runErr)
		return runErr
	}

	log.Printf("INFO: Node %d: Synchronet JS door '%s' completed normally",
		ctx.NodeNumber, ctx.DoorName)
	return nil
}
