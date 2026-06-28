package menu

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// convertSubsToCP437 converts all substitution map values from UTF-8 to CP437 bytes.
// Runes with no CP437 equivalent fall back to a visual ASCII lookalike if one exists,
// then '?'. This is used when outputMode is CP437 so that substituted text in MSGHDR
// templates renders correctly on CP437 terminals instead of displaying raw UTF-8 bytes.
func convertSubsToCP437(subs map[byte]string) map[byte]string {
	result := make(map[byte]string, len(subs))
	for k, v := range subs {
		result[k] = toCP437Safe(v)
	}
	return result
}

// buildMsgSubstitutions creates the Pascal-style substitution map for MSGHDR templates.
func buildMsgSubstitutions(msg *message.DisplayMessage, areaTag string, msgNum, totalMsgs int, userLevel int, includeNoteInFrom bool, replyCount int, confName string, areaName string, msgMgr *message.MessageManager, areaID int, userMgr *user.UserMgr, nodeNumber int, v3netStatus V3NetStatusProvider) map[byte]string {
	// Import jam constants
	const (
		msgTypeEcho = 0x01000000
		msgTypeNet  = 0x02000000
		msgPrivate  = 0x00000004
		msgRead     = 0x00000008
		msgSent     = 0x00000010
	)

	// Build message status flags (also used for note display)
	isEcho := (msg.Attributes & msgTypeEcho) != 0
	isNet := (msg.Attributes & msgTypeNet) != 0
	isSent := (msg.Attributes & msgSent) != 0
	isRead := (msg.Attributes & msgRead) != 0
	isPrivate := (msg.Attributes & msgPrivate) != 0

	// Check if this area is V3Net-networked.
	v3netNetwork := ""
	v3netNodeID := ""
	if v3netStatus != nil {
		v3netNetwork = v3netStatus.NetworkForArea(areaID)
		if v3netNetwork != "" {
			v3netNodeID = v3netStatus.NodeID()
		}
	}
	isV3Net := v3netNetwork != ""

	// Look up the message author's user note from users.json
	userNoteToUse := ""
	if userMgr != nil {
		if authorUser, found := userMgr.GetUser(msg.From); found {
			userNoteToUse = authorUser.PrivateNote
		}
	}

	// Truncate user note if too long (max 25 characters for display)
	const maxUserNoteLen = 25
	truncatedNote := userNoteToUse
	if len(userNoteToUse) > maxUserNoteLen {
		truncatedNote = userNoteToUse[:maxUserNoteLen-3] + "..."
	}

	// Build From field with user note and/or network address.
	// Names are truncated if needed to preserve the address suffix,
	// preventing display like "(21:4/158" with a missing closing paren.
	fromStr := msg.From
	if includeNoteInFrom && truncatedNote != "" && msg.OrigAddr != "" {
		// Both user note and FidoNet address
		fromStr = fmt.Sprintf("%s \"%s\" (%s)", msg.From, truncatedNote, msg.OrigAddr)
	} else if includeNoteInFrom && truncatedNote != "" {
		// Just user note
		fromStr = fmt.Sprintf("%s \"%s\"", msg.From, truncatedNote)
	} else if msg.OrigAddr != "" {
		// FidoNet address
		fromStr = buildNameWithAddr(msg.From, msg.OrigAddr)
	}

	// Build To field with FidoNet destination address if available
	toStr := msg.To
	if msg.DestAddr != "" {
		toStr = buildNameWithAddr(msg.To, msg.DestAddr)
	}

	// Build message status string from message attributes and network type.
	var statusParts []string

	if isV3Net {
		if isSent {
			statusParts = append(statusParts, "V3NET SENT")
		} else {
			statusParts = append(statusParts, "V3NET")
		}
	} else if isEcho {
		if isSent {
			statusParts = append(statusParts, "ECHOMAIL SENT")
		} else {
			statusParts = append(statusParts, "ECHOMAIL")
		}
	} else if isNet {
		if isSent {
			statusParts = append(statusParts, "NETMAIL SENT")
		} else {
			statusParts = append(statusParts, "NETMAIL UNSENT")
		}
	} else {
		statusParts = append(statusParts, "LOCAL")
	}

	// Add additional status flags
	if isRead {
		statusParts = append(statusParts, "READ")
	}
	if isPrivate {
		statusParts = append(statusParts, "PRIVATE")
	}

	msgStatusStr := strings.Join(statusParts, " ")

	// Determine reply-to display: use JAM header ReplyTo (message number) first,
	// then fall back to MSGID index lookup, then "None".
	replyStr := "None"
	if msg.ReplyToNum > 0 {
		// JAM header already has the parent message number (set by linker/tosser)
		replyStr = strconv.Itoa(msg.ReplyToNum)
	} else if msg.ReplyID != "" {
		// Header ReplyTo not set — try MSGID index lookup as fallback
		if replyMsgNum := findMessageByMSGID(msgMgr, areaID, msg.ReplyID); replyMsgNum > 0 {
			replyStr = strconv.Itoa(replyMsgNum)
		}
		// If neither works, leave as "None" — no confusing text for users
	}

	// Origin address: FTN address for FTN areas, V3Net node ID for V3Net areas.
	originAddr := msg.OrigAddr
	if isV3Net && originAddr == "" {
		originAddr = v3netNodeID
	}

	return map[byte]string{
		'B': areaTag,
		'T': msg.Subject,
		'F': fromStr,                 // From with network address
		'S': toStr,                   // To with network address
		'U': userNoteToUse,           // User note from user profile (local only)
		'M': msgStatusStr,            // Message status (LOCAL, ECHOMAIL, NETMAIL, V3NET, etc.)
		'L': strconv.Itoa(userLevel), // User level/access level
		'#': strconv.Itoa(msgNum),
		'N': strconv.Itoa(totalMsgs),
		'C': fmt.Sprintf("[%d/%d]", msgNum, totalMsgs), // Message count display
		'D': msg.DateTime.Format("01/02/06"),
		'W': msg.DateTime.Format("3:04 pm"),
		'P': replyStr,
		'E': strconv.Itoa(replyCount),
		'O': originAddr,                                                            // Origin: FTN address or V3Net node ID
		'A': msg.DestAddr,                                                          // Destination address
		'Z': fmt.Sprintf("%s > %s", confName, areaName),                            // Conference > Area Name
		'V': fmt.Sprintf("%d of %d", msgNum, totalMsgs),                            // Verbose count: "1 of 24"
		'X': fmt.Sprintf("%s > %s [%d/%d]", confName, areaName, msgNum, totalMsgs), // Conference > Area [current/total]
		'K': strconv.Itoa(nodeNumber),                                              // Node number
		'I': v3netNetwork,                                                          // V3Net network name (empty if not V3Net)
	}
}

// buildNameWithAddr combines a display name with an FTN address suffix like "Name (21:4/158)".
// If the combined string exceeds 45 visible characters, the name is truncated to ensure
// the full address (with closing parenthesis) is always visible in constrained template fields.
func buildNameWithAddr(name, addr string) string {
	const maxLen = 45
	suffix := " (" + addr + ")"
	combined := name + suffix
	if len(combined) <= maxLen {
		return combined
	}
	// Truncate name to fit, preserving the address suffix
	nameMax := maxLen - len(suffix)
	if nameMax < 3 {
		nameMax = 3
	}
	return name[:nameMax] + suffix
}

// buildAutoWidths calculates the maximum display width for each placeholder code.
// Used by the @CODE*@ auto-width modifier so templates don't need hardcoded widths.
// Width is based on the maximum possible value for each code in the current context:
//   - Numeric codes (#, N, C): based on totalMsgs digit count
//   - Fixed-format codes (D, W): known max format lengths
//   - Context codes (Z, X): based on current conference/area names + max count width
//   - All others: width of the current substitution value
func buildAutoWidths(subs map[byte]string, totalMsgs int, termWidth int) map[byte]int {
	widths := make(map[byte]int)

	maxMsgNumWidth := len(strconv.Itoa(totalMsgs))

	// Fixed-format codes
	widths['D'] = 8 // MM/DD/YY always 8
	widths['W'] = 8 // max "12:00 pm" = 8

	// Numeric codes: pad to width of largest possible message number
	widths['#'] = maxMsgNumWidth
	widths['N'] = maxMsgNumWidth

	// Count display [current/total]: max when current = totalMsgs
	maxCountStr := fmt.Sprintf("[%d/%d]", totalMsgs, totalMsgs)
	widths['C'] = len(maxCountStr)

	// Verbose count "X of Y": max when current = totalMsgs
	maxVerboseStr := fmt.Sprintf("%d of %d", totalMsgs, totalMsgs)
	widths['V'] = len(maxVerboseStr)

	// Z = "confName > areaName" (same for all messages in area)
	if zVal, ok := subs['Z']; ok {
		widths['Z'] = len(zVal)
		// X = Z + " " + [current/total], max when current = totalMsgs
		widths['X'] = len(zVal) + 1 + len(maxCountStr)
	}

	// Gap fill: target width is terminal width minus 1 to avoid auto-wrap
	// at column 80 which creates blank lines for sequential text output
	if termWidth > 1 {
		widths['G'] = termWidth - 1
	}

	// All other codes: use current value length
	for code, val := range subs {
		if _, exists := widths[code]; !exists {
			widths[code] = len(val)
		}
	}

	return widths
}

// findHeaderEndRow parses processed ANSI bytes and tracks cursor position
// through ESC[row;colH commands and newlines to find the maximum row used.
// This correctly handles MSGHDR templates that use absolute cursor positioning.
func findHeaderEndRow(data []byte) int {
	maxRow := 1
	curRow := 1
	i := 0
	for i < len(data) {
		if data[i] == '\n' {
			curRow++
			if curRow > maxRow {
				maxRow = curRow
			}
			i++
			continue
		}
		// Check for ESC[ sequences
		if data[i] == 0x1B && i+1 < len(data) && data[i+1] == '[' {
			i += 2
			// Parse numeric params
			params := ""
			for i < len(data) && (data[i] == ';' || (data[i] >= '0' && data[i] <= '9')) {
				params += string(data[i])
				i++
			}
			if i < len(data) {
				cmd := data[i]
				i++
				if cmd == 'H' || cmd == 'f' { // Cursor position
					parts := strings.Split(params, ";")
					if len(parts) >= 1 && parts[0] != "" {
						row, err := strconv.Atoi(parts[0])
						if err == nil {
							curRow = row
							if curRow > maxRow {
								maxRow = curRow
							}
						}
					}
				}
				// Skip other commands - they don't change row
			}
			continue
		}
		i++
	}
	return maxRow
}
