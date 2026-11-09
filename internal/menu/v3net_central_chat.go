package menu

import (
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// V3Net Central phase 3: network chat, and the caller's agreement to take
// part in it. The sysop's Chat switch decides which networks a caller can
// chat on at all (the leaf hides the rest; see ChatLeafProvider). On
// those, a caller joins only after agreeing to the notice below, once,
// and can withdraw under My Preferences. The notice says exactly what
// leaves the board: their handle and this board's name, with each line
// they type. No Callers Default applies to chat.

// centralChatOfferedNetworks returns the names of the networks where the
// SysOp offers network chat, served by the hub or not yet known to be.
func centralChatOfferedNetworks(p V3NetCentralProvider) (names []string) {
	if p == nil {
		return nil
	}
	for _, n := range p.CentralNetworks() {
		if n.ChatOffered() && (n.Chat.Supported || !n.SupportKnown) {
			names = append(names, n.Network)
		}
	}
	return names
}

// centralChatAvailable reports whether callers can use network chat on
// some network now: offered by the SysOp and served by the hub.
func centralChatAvailable(p V3NetCentralProvider) bool {
	if p == nil {
		return false
	}
	for _, n := range p.CentralNetworks() {
		if n.ChatAvailable() {
			return true
		}
	}
	return false
}

// chatConsentStored returns the caller's network chat agreement as the
// stored account has it: consent is owned by the stored record, so a
// withdrawal made on another node counts here. Without a user manager
// (tests), the session's copy is read.
func chatConsentStored(c *cmdCtx) bool {
	u := c.currentUser
	if u == nil {
		return false
	}
	if c.userManager != nil {
		if stored, ok := c.userManager.GetUserByID(u.ID); ok {
			centralConsentFrom(u, stored)
		}
	}
	return u.CentralChat
}

// setChatConsent records the caller's answer to the network chat notice
// on the stored account (or the session's copy without a user manager).
// Saving marks the account as having chosen, either way.
func setChatConsent(c *cmdCtx, agreed bool) error {
	mutate := func(x *user.User) {
		x.CentralChat = agreed
		x.CentralChatChosen = true
	}
	if c.userManager == nil {
		mutate(c.currentUser)
		return nil
	}
	saved, err := c.userManager.SetCentralConsent(c.currentUser.ID, mutate)
	if err != nil {
		slog.Error("failed to save V3Net chat consent", "node", c.nodeNumber, "error", err)
		return err
	}
	centralConsentFrom(c.currentUser, saved)
	return nil
}

// chatNoticeBoard is the board name the notice shows other boards seeing.
func chatNoticeBoard(e *MenuExecutor) string {
	board := strings.TrimSpace(e.GetServerConfig().BoardName)
	if board == "" {
		board = "this board"
	}
	return board
}

// chatConsentNoticeText is the network chat notice: what other boards see,
// what the hub keeps and who moderates, for a caller with handle on board.
// It ends without a prompt.
func chatConsentNoticeText(handle, board string, width int) string {
	var b strings.Builder
	b.WriteString(centralHeader("Network Chat", width))
	w := func(line string) { b.Write(ansi.ReplacePipeCodes([]byte(line + "\r\n"))) }
	w("|15  Network chat puts you in a room with callers on other boards.|07")
	w("")
	w("|03  What other boards see|07")
	w("|07  Your handle |15" + v3netClip(centralPreviewHandle(handle), 30) + "|07 and this board's name |11" + v3netClip(board, 30) + "|07,")
	w("|07  in the room's user list while you are there and on every line you")
	w("|07  type. A private message goes to the hub and to the recipient's board.")
	w("|08  Never your real name, location, user number, or what you are doing.|07")
	w("")
	w("|03  What is kept|07")
	w("|07  The hub keeps room messages as history for callers who join later,")
	w("|07  and private messages for the same time; a ViSiON/3 hub keeps 7 days.")
	w("|07  Room history can be read by anyone who knows the hub's address, without")
	w("|07  joining: treat what you say in a room as public.")
	w("")
	w("|03  Who moderates|07")
	w("|07  Your SysOp decides whether this board takes part; the hub's operator")
	w("|07  decides which boards may. The hub limits how fast anyone can post.")
	w("|08  You can turn network chat off at any time under Preferences (V3Net|07")
	w("|08  Central, or V3Net Sharing in Konfig), and leave a room with /q.|07")
	w("")
	return b.String()
}

// chatConsentNotice shows the network chat notice and asks the caller to
// take part. Y records their agreement and returns true; anything else
// returns false and records nothing, so they are asked again next time.
// next and err are as v3netPromptLine returns them.
func chatConsentNotice(c *cmdCtx) (agreed bool, next string, err error) {
	e, s, terminal, outputMode := c.e, c.s, c.terminal, c.outputMode
	terminalio.WriteProcessedBytes(terminal, []byte(chatConsentNoticeText(c.currentUser.Handle, chatNoticeBoard(e), c.termWidth)), outputMode)
	line, next, err := v3netPromptLine(s, terminal, outputMode, "|07  Take part in network chat? |08(|07Y|08/|07N|08)|07: ")
	if err != nil {
		return false, next, err
	}
	if !strings.EqualFold(line, "y") && !strings.EqualFold(line, "yes") {
		return false, "", nil
	}
	if err := setChatConsent(c, true); err != nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|04  Could not save that; nothing changed.|07\r\n")), outputMode)
		return false, "", nil
	}
	return true, "", nil
}
