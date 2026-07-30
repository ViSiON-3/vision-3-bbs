package jam

import "strings"

// Building the JAM subfield list that accompanies a message header, and the
// origin-line parsing that feeds it.

// buildSubfields assembles the standard subfield list for a message.
func buildSubfields(msg *Message) []Subfield {
	var sfs []Subfield
	if msg.OrigAddr != "" {
		sfs = append(sfs, CreateSubfield(SfldOAddress, msg.OrigAddr))
	}
	if msg.DestAddr != "" {
		sfs = append(sfs, CreateSubfield(SfldDAddress, msg.DestAddr))
	}
	if msg.From != "" {
		sfs = append(sfs, CreateSubfield(SfldSenderName, msg.From))
	}
	if msg.To != "" {
		sfs = append(sfs, CreateSubfield(SfldReceiverName, msg.To))
	}
	if msg.Subject != "" {
		sfs = append(sfs, CreateSubfield(SfldSubject, msg.Subject))
	}
	if msg.MsgID != "" {
		sfs = append(sfs, CreateSubfield(SfldMsgID, msg.MsgID))
	}
	if msg.ReplyID != "" {
		sfs = append(sfs, CreateSubfield(SfldReplyID, msg.ReplyID))
	}
	if msg.PID != "" {
		sfs = append(sfs, CreateSubfield(SfldPID, msg.PID))
	}
	for _, kludge := range msg.Kludges {
		sfs = append(sfs, CreateSubfield(SfldFTSKludge, kludge))
	}
	return sfs
}

// extractAddressFromOriginLine parses a FidoNet address from the origin line.
// Format: " * Origin: BBS Name (address)"
func extractAddressFromOriginLine(text string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "* Origin:") {
			start := strings.LastIndex(line, "(")
			end := strings.LastIndex(line, ")")
			if start != -1 && end != -1 && end > start {
				return strings.TrimSpace(line[start+1 : end])
			}
		}
	}
	return ""
}
