package menu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// qwkBBSID returns a short BBS identifier derived from the board name
// (alphanumeric, max 8 chars, uppercase), falling back to "BBS".
func qwkBBSID(boardName string) string {
	if id := config.NormalizeQWKID(boardName); id != "" {
		return id
	}
	return "BBS"
}

// resolveQWKID returns the BBS's QWK packet ID: the explicitly configured ID
// (normalized) if set, otherwise one derived from the board name (qwkBBSID).
func resolveQWKID(cfg config.ServerConfig) string {
	if id := config.NormalizeQWKID(cfg.QWKID); id != "" {
		return id
	}
	return qwkBBSID(cfg.BoardName)
}

// runQWKDownload builds and sends a QWK mail packet to the user.
func runQWKDownload(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	slog.Debug("running QWKDOWNLOAD", "node", nodeNumber)

	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	bbsID := resolveQWKID(e.ServerCfg)
	svc := qwkservice.New(e.MessageMgr, bbsID, e.ServerCfg.BoardName, e.ServerCfg.SysOpName, e.MessageMgr.DataPath())

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|15Building QWK packet...|07\r\n")), outputMode)

	// BuildPacket gathers new messages and returns the packet bytes plus the
	// pending newscan advances. The advances are committed (via CommitExport)
	// only after a successful transfer, so a failed or cancelled download does
	// not move the pointers.
	res, err := svc.BuildPacket(qwkservice.ExportOptions{
		Handle:     currentUser.Handle,
		TaggedTags: currentUser.TaggedMessageAreaTags,
	})
	if err != nil {
		slog.Error("failed to build QWK packet", "node", nodeNumber, "error", err)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error building QWK packet.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return currentUser, "", nil
	}

	if res.MessageCount == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07No new messages to download.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return currentUser, "", nil
	}

	statusMsg := fmt.Sprintf("\r\n|14%d|07 message(s) packed into QWK packet.\r\n", res.MessageCount)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(statusMsg)), outputMode)

	// Prompt user to send or quit
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+e.LoadedStrings.SendQWKPacketPrompt)), outputMode)
	promptInput, promptErr := readLineFromSessionIH(s, terminal)
	if promptErr != nil {
		if errors.Is(promptErr, io.EOF) {
			return nil, "LOGOFF", promptErr
		}
		return currentUser, "", nil
	}
	if strings.ToUpper(strings.TrimSpace(promptInput)) == "Q" {
		return currentUser, "", nil
	}

	// Write packet to temp file
	tmpFile, err := os.CreateTemp("", "qwk-*.zip")
	if err != nil {
		slog.Error("failed to create temp file", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(res.Packet); err != nil {
		tmpFile.Close()
		slog.Error("failed to write packet", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}
	tmpFile.Close()

	// Rename to BBSID.QWK for the transfer
	qwkPath := filepath.Join(filepath.Dir(tmpPath), bbsID+".QWK")
	if err := os.Rename(tmpPath, qwkPath); err != nil {
		slog.Error("rename failed", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}
	defer os.Remove(qwkPath)

	// Protocol selection and send
	proto, ok, protoErr := e.selectTransferProtocol(s, terminal, outputMode)
	if protoErr != nil {
		if errors.Is(protoErr, io.EOF) {
			return nil, "LOGOFF", protoErr
		}
		return currentUser, "", nil
	}
	if !ok {
		return currentUser, "", nil
	}

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("\r\n|15Sending %s.QWK via %s...|07\r\n", bbsID, proto.Name))), outputMode)

	resetSessionIH(s)
	ctx, cancel := e.transferContext(s.Context())
	defer cancel()
	sendErr := proto.ExecuteSend(ctx, s, qwkPath)
	time.Sleep(250 * time.Millisecond)
	getSessionIH(s)

	if sendErr != nil {
		if errors.Is(sendErr, transfer.ErrBinaryNotFound) {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Transfer program not found!|07\r\n")), outputMode)
		} else {
			slog.Warn("QWK download transfer failed", "node", nodeNumber, "error", sendErr)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Transfer failed.|07\r\n")), outputMode)
		}
	} else {
		// Transfer succeeded — commit the newscan pointer advances.
		svc.CommitExport(currentUser.Handle, res)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|10QWK packet sent successfully.|07\r\n")), outputMode)
	}
	time.Sleep(2 * time.Second)

	return currentUser, "", nil
}

// runQWKUpload receives and processes a QWK REP packet from the user.
func runQWKUpload(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	slog.Debug("running QWKUPLOAD", "node", nodeNumber)

	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	bbsID := resolveQWKID(e.ServerCfg)

	// Protocol selection
	proto, ok, protoErr := e.selectTransferProtocol(s, terminal, outputMode)
	if protoErr != nil {
		if errors.Is(protoErr, io.EOF) {
			return nil, "LOGOFF", protoErr
		}
		return currentUser, "", nil
	}
	if !ok {
		return currentUser, "", nil
	}

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf("\r\n|11Send your %s.REP file via %s now.|07\r\n", bbsID, proto.Name))), outputMode)

	// Receive into temp directory
	incomingDir, err := os.MkdirTemp("", "qwk-rep-*")
	if err != nil {
		slog.Error("failed to create temp dir", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}
	defer os.RemoveAll(incomingDir)

	resetSessionIH(s)
	ctx, cancel := e.transferContext(s.Context())
	defer cancel()
	recvErr := proto.ExecuteReceive(ctx, s, incomingDir)
	time.Sleep(250 * time.Millisecond)
	getSessionIH(s)

	if recvErr != nil && !errors.Is(recvErr, context.Canceled) {
		slog.Warn("QWK REP receive error, checking for files anyway", "node", nodeNumber, "error", recvErr)
	}

	// Find the .REP file
	repPath := findREPFile(incomingDir, bbsID)
	if repPath == "" {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01No REP packet received.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return currentUser, "", nil
	}

	// Process the REP packet
	repData, err := os.ReadFile(repPath)
	if err != nil {
		slog.Error("failed to read REP", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	svc := qwkservice.New(e.MessageMgr, bbsID, e.ServerCfg.BoardName, e.ServerCfg.SysOpName, e.MessageMgr.DataPath())

	// The service owns parsing and posting; the menu supplies the ACS gate and
	// per-area progress output as callbacks so terminal/UI concerns stay here.
	importRes, err := svc.ImportREP(repData, qwkservice.ImportOptions{
		Handle:    currentUser.Handle,
		Signature: currentUser.AutoSignature,
		Authorize: func(area *message.MessageArea) bool {
			return area.ACSWrite == "" || checkACS(area.ACSWrite, currentUser, s, terminal, sessionStartTime)
		},
		Notify: func(area *message.MessageArea) {
			postMsg := strings.ReplaceAll(e.LoadedStrings.PostingQWKMsg, "|BN", area.Name)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+postMsg)), outputMode)
		},
	})
	if err != nil {
		if errors.Is(err, qwkservice.ErrWrongBBS) {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01This REP packet is addressed to another BBS.|07\r\n")), outputMode)
			time.Sleep(2 * time.Second)
			return currentUser, "", nil
		}
		slog.Error("failed to process REP", "node", nodeNumber, "error", err)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error processing REP packet.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return currentUser, "", nil
	}

	if importRes.Duplicate > 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07This packet was already uploaded — nothing posted.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return currentUser, "", nil
	}

	if importRes.Posted+importRes.Skipped == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07REP packet contains no messages.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return currentUser, "", nil
	}

	posted := importRes.Posted

	// Update user stats
	if posted > 0 && userManager != nil {
		currentUser.MessagesPosted += posted
		if updateErr := userManager.UpdateUser(currentUser); updateErr != nil {
			slog.Error("failed to update user stats", "node", nodeNumber, "error", updateErr)
		}
	}

	statusMsg := strings.ReplaceAll(e.LoadedStrings.TotalQWKAdded, "|TO", fmt.Sprintf("%d", posted))
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+statusMsg+"\r\n")), outputMode)
	time.Sleep(2 * time.Second)

	return currentUser, "", nil
}

// findREPFile looks for a .REP file in the directory, matching the BBS ID.
func findREPFile(dir string, bbsID string) string {
	expected := strings.ToUpper(bbsID) + ".REP"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), expected) {
			return filepath.Join(dir, e.Name())
		}
	}
	// Fall back: any .REP file
	for _, e := range entries {
		if strings.HasSuffix(strings.ToUpper(e.Name()), ".REP") {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}
