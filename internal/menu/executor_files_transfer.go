package menu

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/telnetserver"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"
	"golang.org/x/term"
)

// isTelnetSession returns true when s was established over a raw telnet connection.
func isTelnetSession(s ssh.Session) bool {
	_, ok := s.(*telnetserver.TelnetSessionAdapter)
	return ok
}

// selectTransferProtocol displays the available transfer protocols filtered for the
// current connection type, then prompts the user to choose one by key.
//
// Rules:
//   - Protocols with connection_type "" are shown on all connections.
//   - Protocols with connection_type "ssh" are shown on SSH sessions only.
//   - Protocols with connection_type "telnet" are shown on telnet sessions only.
//   - Pressing Enter selects the default protocol.
//   - Typing Q cancels. An unrecognised key re-prompts — no silent fallback.
//
// Returns (selected, true, nil) on selection, (zero, false, nil) on cancel,
// or (zero, false, err) on I/O error.
func (e *MenuExecutor) selectTransferProtocol(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode) (transfer.ProtocolConfig, bool, error) {
	// Filter protocols for this connection type.
	connType := transfer.ConnTypeSSH
	if isTelnetSession(s) {
		connType = transfer.ConnTypeTelnet
	}
	var available []transfer.ProtocolConfig
	for _, p := range e.Protocols {
		if p.ConnectionType == transfer.ConnTypeAny || p.ConnectionType == connType {
			available = append(available, p)
		}
	}
	if len(available) == 0 {
		return transfer.ProtocolConfig{}, false, fmt.Errorf("no transfer protocols configured for this connection type")
	}

	defaultProto, hasDefault := transfer.DefaultProtocol(available)
	if !hasDefault {
		defaultProto = available[0]
	}

	// Build the menu string once — reused on re-prompt.
	var menu strings.Builder
	menu.WriteString("\r\n|15Transfer Protocols:|07\r\n\r\n")
	for _, p := range available {
		if p.Default {
			fmt.Fprintf(&menu, "  |15[|14%-3s|15]|07 %-22s |08(default)|07\r\n", p.Key, p.Name)
		} else {
			fmt.Fprintf(&menu, "  |15[|14%-3s|15]|07 %s\r\n", p.Key, p.Name)
		}
	}
	menuBytes := ansi.ReplacePipeCodes([]byte(menu.String()))

	for {
		terminalio.WriteProcessedBytes(terminal, menuBytes, outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|07Select protocol |15[%s]|07, or |15Q|07 to cancel: ", defaultProto.Key))), outputMode)

		input, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			return transfer.ProtocolConfig{}, false, err
		}
		input = strings.TrimSpace(input)

		if strings.ToUpper(input) == "Q" {
			return transfer.ProtocolConfig{}, false, nil
		}
		if input == "" {
			return defaultProto, true, nil
		}

		p, found := transfer.FindProtocol(available, input)
		if found {
			return p, true, nil
		}
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("\r\n|01Unknown protocol %q — please choose from the list above.|07\r\n", strings.ToUpper(input)))), outputMode)
	}
}

// runTransferSend executes a protocol send for the given file paths. It handles
// resetSessionIH/getSessionIH, batch vs one-at-a-time logic, ExecuteSend, error
// handling (including ErrBinaryNotFound), and IncrementDownloadCount.
// fileIDs must match paths in order (paths[i] corresponds to fileIDs[i]).
// Returns successCount and failCount.
func (e *MenuExecutor) runTransferSend(s ssh.Session, terminal *term.Terminal, proto transfer.ProtocolConfig, paths []string, fileIDs []uuid.UUID, outputMode ansi.OutputMode, nodeNumber int) (successCount, failCount int) {
	if len(paths) == 0 {
		return 0, 0
	}

	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}

	resetSessionIH(s)
	defer func() {
		time.Sleep(250 * time.Millisecond)
		getSessionIH(s)
	}()

	if proto.BatchSend && len(paths) > 1 {
		// Batch: single transfer session
		slog.Info("batch sending files", "node", nodeNumber, "count", len(paths), "protocol", proto.Name, "names", names)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("\r\n|15Initiating %s batch transfer (%d files)...|07\r\n", proto.Name, len(paths)))), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|07Please start the receive function in your terminal.\r\n")), outputMode)

		ctx, cancel := e.transferContext(s.Context())
		defer cancel()
		transferErr := proto.ExecuteSend(ctx, s, paths...)
		if transferErr != nil {
			slog.Error("batch send failed", "node", nodeNumber, "protocol", proto.Name, "error", transferErr)
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			if errors.Is(transferErr, transfer.ErrBinaryNotFound) {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01File transfer program not found!|07\r\n|07The SysOp needs to install the transfer binary (sexyz).\r\n|07See docs/sysop/files/file-transfer.md for setup instructions.\r\n")), outputMode)
			} else {
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|01Transfer failed or was cancelled.\r\n")), outputMode)
			}
			return 0, len(paths)
		}
		slog.Info("batch send completed", "node", nodeNumber, "protocol", proto.Name)
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|07Transfer complete.\r\n")), outputMode)
		for _, id := range fileIDs {
			if id != uuid.Nil {
				if err := e.FileMgr.IncrementDownloadCount(id); err != nil {
					slog.Warn("failed to increment download count", "node", nodeNumber, "fileID", id, "error", err)
				}
			}
		}
		return len(paths), 0
	}

	// One-at-a-time
	slog.Info("sending files one-at-a-time", "node", nodeNumber, "count", len(paths), "protocol", proto.Name)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("\r\n|15Initiating %s transfer (%d file(s), one at a time)...|07\r\n", proto.Name, len(paths)))), outputMode)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|07Prepare your terminal to receive each file.\r\n")), outputMode)

	for i, p := range paths {
		ctx, cancel := e.transferContext(s.Context())
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|15[%d/%d]|07 Sending: |14%s|07...", i+1, len(paths), names[i]))), outputMode)
		sendErr := proto.ExecuteSend(ctx, s, p)
		cancel()
		if sendErr != nil {
			slog.Error("send failed", "node", nodeNumber, "protocol", proto.Name, "name", names[i], "error", sendErr)
			if errors.Is(sendErr, transfer.ErrBinaryNotFound) {
				terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01File transfer program not found!|07\r\n|07The SysOp needs to install the transfer binary (sexyz).\r\n|07See docs/sysop/files/file-transfer.md for setup instructions.\r\n")), outputMode)
				return successCount, failCount + (len(paths) - i)
			}
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|15[%d/%d]|07 |14%s|07: |01FAILED|07\r\n", i+1, len(paths), names[i]))), outputMode)
			failCount++
			continue
		}
		slog.Info("file sent", "node", nodeNumber, "protocol", proto.Name, "name", names[i])
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|15[%d/%d]|07 |14%s|07: |02OK|07\r\n", i+1, len(paths), names[i]))), outputMode)
		successCount++
		if i < len(fileIDs) && fileIDs[i] != uuid.Nil {
			if err := e.FileMgr.IncrementDownloadCount(fileIDs[i]); err != nil {
				slog.Warn("failed to increment download count", "node", nodeNumber, "fileID", fileIDs[i], "error", err)
			}
		}
	}
	return successCount, failCount
}
