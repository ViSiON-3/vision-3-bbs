package menu

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// composeRecipientKind is how COMPOSEMSG treats the To: field of an area.
type composeRecipientKind int

const (
	// recipientPublic: To defaults to "All" and a blank answer means "All".
	recipientPublic composeRecipientKind = iota
	// recipientPrivate: the PRIVMAIL area. To must name an existing user,
	// and the message is saved with the private flag so it reaches their
	// mailbox (see runSendPrivateMail).
	recipientPrivate
	// recipientNetmail: To must name a person and carry an FTN address, or
	// the message has no destination and the tosser cannot route it.
	recipientNetmail
)

// composeRecipientKindFor returns how COMPOSEMSG should ask for the recipient
// in area. Netmail is detected the way the message base does when it writes
// the message, so the two cannot disagree.
func composeRecipientKindFor(area *message.MessageArea) composeRecipientKind {
	switch {
	case strings.EqualFold(area.Tag, "PRIVMAIL"):
		return recipientPrivate
	case isNetmailArea(area):
		return recipientNetmail
	default:
		return recipientPublic
	}
}

// isNetmailArea reports whether area holds netmail, detected the way the
// message base does when it writes a message.
func isNetmailArea(area *message.MessageArea) bool {
	return area != nil && jam.DetermineMessageType(area.AreaType, area.EchoTag).IsNetmail()
}

const (
	// netmailNameMaxLen is the FTN limit on a netmail's to-name.
	netmailNameMaxLen = 36
	// netmailAddrMaxLen fits the longest address jam.ParseAddress accepts,
	// 65535:65535/65535.65535.
	netmailAddrMaxLen = 23
	// netmailToMaxLen fits a full-length name, "@" and a full-length
	// address. A tighter limit would cut an inline address short, and a cut
	// address can still parse as a different node.
	netmailToMaxLen = netmailNameMaxLen + 1 + netmailAddrMaxLen
)

// promptComposeRecipient asks for the To: field of a new message in area. It
// returns to, the value to store (for netmail, "name@zone:net/node"), and name,
// the addressee as the editor header shows it. aborted is set when the caller
// backed out, including by leaving a required field blank, which abandons the
// post the same way a blank title does.
//
// A public area keeps the classic behavior: To defaults to "All". A private
// or netmail message has to go to someone, so it starts empty; an unknown
// user or an unparseable address is reported and asked again.
func (e *MenuExecutor) promptComposeRecipient(s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, area *message.MessageArea, outputMode ansi.OutputMode, nodeNumber, termWidth, termHeight int) (to, name string, aborted bool, err error) {
	toPrompt := e.Strings().MsgToStr
	if toPrompt == "" {
		toPrompt = "|07To: |15"
	}
	say := func(text string) {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(text)), outputMode)
	}
	ask := func(prompt string, maxLen int, def, field string) (string, bool, error) {
		val, aborted, err := e.promptComposeField(s, terminal, prompt, maxLen, def, field, outputMode, nodeNumber, termWidth, termHeight)
		return strings.TrimSpace(val), aborted, err
	}

	switch composeRecipientKindFor(area) {
	case recipientPrivate:
		for {
			val, aborted, err := ask(toPrompt, 24, "", "'to'")
			if err != nil || aborted {
				return "", "", aborted, err
			}
			if val == "" {
				showPostAborted(terminal, outputMode)
				return "", "", true, nil
			}
			if userManager != nil {
				if u, ok := userManager.GetUser(val); ok && u != nil && !u.DeletedUser {
					return u.Handle, u.Handle, false, nil
				}
			}
			say(fmt.Sprintf("|01User '%s' not found.|07\r\n", val))
		}

	case recipientNetmail:
		var name, addr string
		for {
			val, aborted, err := ask(toPrompt, netmailToMaxLen, "", "'to'")
			if err != nil || aborted {
				return "", "", aborted, err
			}
			if val == "" {
				showPostAborted(terminal, outputMode)
				return "", "", true, nil
			}
			name, addr = message.SplitNetmailTo(val)
			if addr == "" {
				// SplitNetmailTo keeps a bad address as part of the name, so
				// catch an attempt at one here; otherwise the mail would be
				// addressed to "Joe@21:1/100x". An "@" followed by something
				// else is left alone, as handles can contain one.
				if at := strings.LastIndex(val, "@"); at >= 0 && looksLikeFTNAddress(val[at+1:]) {
					if strings.TrimSpace(val[:at]) == "" {
						say("|01Enter a name before the address.|07\r\n")
					} else {
						say(fmt.Sprintf("|01'%s' is not an FTN address, e.g. 1:234/567.|07\r\n", strings.TrimSpace(val[at+1:])))
					}
					continue
				}
			}
			if utf8.RuneCountInString(name) <= netmailNameMaxLen {
				break
			}
			say(fmt.Sprintf("|01Name is too long for netmail (%d characters at most).|07\r\n", netmailNameMaxLen))
		}
		for {
			if addr == "" {
				a, aborted, err := ask("|07Address (zone:net/node): |15", netmailAddrMaxLen, "", "netmail address")
				if err != nil || aborted {
					return "", "", aborted, err
				}
				if a == "" {
					showPostAborted(terminal, outputMode)
					return "", "", true, nil
				}
				if _, perr := jam.ParseAddress(a); perr != nil {
					say(fmt.Sprintf("|01'%s' is not an FTN address, e.g. 1:234/567.|07\r\n", a))
					continue
				}
				addr = a
			}
			// The tosser hands this area's netmail to its own network's
			// links whatever the zone, so an address in another zone is
			// probably a mistake. Ask rather than refuse: some networks
			// span several zones.
			if ownZone, ok := netmailZone(area); ok && addrZone(addr) != ownZone {
				network := area.Network
				if network == "" {
					network = "this network"
				}
				q := fmt.Sprintf("|07%s is not in %s (zone %d). Send anyway? @", addr, network, ownZone)
				yes, err := e.PromptYesNo(s, terminal, q, outputMode, nodeNumber, termWidth, termHeight, false)
				if err != nil {
					return "", "", false, err
				}
				if !yes {
					addr = "" // ask for the address again
					continue
				}
			}
			send, err := e.confirmNetmailNode(s, terminal, area, addr, outputMode, nodeNumber, termWidth, termHeight)
			if err != nil {
				return "", "", false, err
			}
			if send {
				break
			}
			addr = ""
		}
		return name + "@" + addr, name, false, nil

	default:
		val, aborted, err := ask(toPrompt, 24, "All", "'to'")
		if err != nil || aborted {
			return "", "", aborted, err
		}
		if val == "" {
			val = "All"
		}
		return val, val, false, nil
	}
}

// confirmNetmailNode looks addr up in the nodelist of area's network and
// says which system the netmail is going to. An address the nodelist does
// not list, or lists as Down, is asked about rather than refused: the list
// can be a week old, and the mail goes through the hub either way. send is
// false when the caller answered No and the address should be asked again.
//
// Nothing is shown or asked when the network has no compiled nodelist, or
// when the list does not cover the address's zone.
func (e *MenuExecutor) confirmNetmailNode(s ssh.Session, terminal *term.Terminal, area *message.MessageArea, addr string, outputMode ansi.OutputMode, nodeNumber, termWidth, termHeight int) (send bool, err error) {
	if e.Nodelists == nil || area.Network == "" {
		return true, nil
	}
	a, err := ftn.ParseAddress(addr)
	if err != nil {
		return true, nil
	}
	list := e.Nodelists.List(area.Network)
	if list == nil || !list.HasZone(a.Zone) {
		return true, nil
	}
	say := func(text string) {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(text)), outputMode)
	}
	ask := func(q string) (bool, error) {
		return e.PromptYesNo(s, terminal, q, outputMode, nodeNumber, termWidth, termHeight, false)
	}

	node, ok := e.Nodelists.Lookup(area.Network, a)
	if !ok {
		listed := a
		listed.Point = 0
		what := addr
		if a.Point != 0 {
			what = fmt.Sprintf("%s (the node %s)", addr, listed)
		}
		return ask(fmt.Sprintf("|07%s is not in the %s nodelist%s. Send anyway? @", what, area.Network, nodelistDated(list)))
	}

	system := "|15" + node.Name + "|07"
	if node.Location != "" {
		system += ", " + node.Location
	}
	if node.Sysop != "" {
		system += " (" + node.Sysop + ")"
	}
	if a.Point != 0 {
		say(fmt.Sprintf("|07Sending to a point of %s\r\n", system))
	} else {
		say(fmt.Sprintf("|07Sending to %s\r\n", system))
	}

	switch node.Status {
	case "Down":
		return ask(fmt.Sprintf("|07%s is listed as Down in the %s nodelist. Send anyway? @", node.Address, area.Network))
	case "Hold":
		say("|08Listed as Hold: its mail waits at its host until it calls in.|07\r\n")
	case "Pvt":
		say("|08Listed as Private: its mail goes through its host.|07\r\n")
	}
	return true, nil
}

// nodelistDated describes a compiled nodelist's date for a prompt.
func nodelistDated(list *ftn.CompiledNodelist) string {
	if list.Date.IsZero() {
		return ""
	}
	return " of " + list.Date.Format("2006-01-02")
}

// netmailZone returns the zone of this BBS's own address on area's network,
// which the area records as its origin address. ok is false when the area
// has none, and then no zone check is made.
func netmailZone(area *message.MessageArea) (zone int, ok bool) {
	a, err := jam.ParseAddress(area.OriginAddr)
	if err != nil {
		return 0, false
	}
	return a.Zone, true
}

// addrZone returns the zone of an address already checked by jam.ParseAddress.
func addrZone(addr string) int {
	a, err := jam.ParseAddress(addr)
	if err != nil {
		return 0
	}
	return a.Zone
}

// looksLikeFTNAddress reports whether s reads as an attempt at an FTN
// address, parseable or not: it starts with a digit or holds ":" or "/".
func looksLikeFTNAddress(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return (s[0] >= '0' && s[0] <= '9') || strings.ContainsAny(s, ":/")
}
