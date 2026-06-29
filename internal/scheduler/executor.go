package scheduler

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// executeEvent runs a scheduled event and returns the result
func (s *Scheduler) executeEvent(ctx context.Context, event config.EventConfig) EventResult {
	result := EventResult{
		EventID:   event.ID,
		StartTime: time.Now(),
	}

	slog.Info("event started", "id", event.ID, "name", event.Name)

	// Build substitutions for placeholders
	substitutions := s.buildSubstitutions(event)

	// Substitute in Arguments
	substitutedArgs := make([]string, len(event.Args))
	for i, arg := range event.Args {
		newArg := arg
		for key, val := range substitutions {
			newArg = strings.ReplaceAll(newArg, key, val)
		}
		substitutedArgs[i] = newArg
	}

	// Substitute in Environment Variables
	substitutedEnv := make(map[string]string)
	if event.EnvironmentVars != nil {
		for key, val := range event.EnvironmentVars {
			newVal := val
			for subKey, subVal := range substitutions {
				newVal = strings.ReplaceAll(newVal, subKey, subVal)
			}
			substitutedEnv[key] = newVal
		}
	}

	// Create command with timeout context
	cmdCtx := ctx
	var cancel context.CancelFunc
	if event.TimeoutSeconds > 0 {
		cmdCtx, cancel = context.WithTimeout(ctx, time.Duration(event.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	// Substitute placeholders in command path
	substitutedCommand := event.Command
	for key, val := range substitutions {
		substitutedCommand = strings.ReplaceAll(substitutedCommand, key, val)
	}

	cmd := exec.CommandContext(cmdCtx, substitutedCommand, substitutedArgs...)

	// Set working directory if specified (with placeholder substitution)
	if event.WorkingDirectory != "" {
		workDir := event.WorkingDirectory
		for key, val := range substitutions {
			workDir = strings.ReplaceAll(workDir, key, val)
		}
		cmd.Dir = workDir
		slog.Debug("setting working directory", "id", event.ID, "dir", cmd.Dir)
	}

	// Set environment variables
	cmd.Env = os.Environ()
	if len(substitutedEnv) > 0 {
		for key, val := range substitutedEnv {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, val))
		}
	}

	// Add standard BBS environment variables
	cmd.Env = append(cmd.Env, fmt.Sprintf("BBS_EVENT_ID=%s", event.ID))
	cmd.Env = append(cmd.Env, fmt.Sprintf("BBS_EVENT_NAME=%s", event.Name))

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute the command
	err := cmd.Run()
	result.EndTime = time.Now()
	result.Output = stdout.String()
	result.ErrorOutput = stderr.String()

	// Determine result status
	if err != nil {
		result.Error = err
		if cmdCtx.Err() == context.DeadlineExceeded {
			result.Success = false
			result.ExitCode = -1
			slog.Error("event timed out", "id", event.ID, "name", event.Name, "timeout_s", event.TimeoutSeconds)
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.Success = false
			result.ExitCode = exitErr.ExitCode()
			slog.Error("event failed", "id", event.ID, "name", event.Name, "exit_code", result.ExitCode)
			if result.ErrorOutput != "" {
				slog.Error("event stderr", "id", event.ID, "stderr", result.ErrorOutput)
			}
		} else {
			result.Success = false
			result.ExitCode = -1
			slog.Error("event failed to start", "id", event.ID, "name", event.Name, "error", err)
		}
	} else {
		result.Success = true
		result.ExitCode = 0
		duration := result.EndTime.Sub(result.StartTime)
		slog.Info("event completed", "id", event.ID, "name", event.Name, "duration_s", duration.Seconds())
		if result.Output != "" {
			slog.Debug("event output", "id", event.ID, "output", result.Output)
		}
	}

	return result
}

// buildSubstitutions creates a map of placeholder substitutions for an event
func (s *Scheduler) buildSubstitutions(event config.EventConfig) map[string]string {
	now := time.Now()

	// Get BBS root directory from current working directory
	// This should be the BBS installation root where the binary is running
	bbsRoot, err := os.Getwd()
	if err != nil {
		slog.Warn("failed to get working directory", "error", err)
		bbsRoot = "."
	}

	return map[string]string{
		"{TIMESTAMP}":  strconv.FormatInt(now.Unix(), 10),
		"{EVENT_ID}":   event.ID,
		"{EVENT_NAME}": event.Name,
		"{BBS_ROOT}":   bbsRoot,
		"{DATE}":       now.Format("2006-01-02"),
		"{TIME}":       now.Format("15:04:05"),
		"{DATETIME}":   now.Format("2006-01-02 15:04:05"),
	}
}
