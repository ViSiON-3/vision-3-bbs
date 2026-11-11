package menu

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The coordinator's Board Roster (#998): which boards a network's Central
// features vouch for. Boards that hold a roster show Central listings,
// activity and alerts only from boards on it, so a hub cannot invent a
// board of its own. The screen lists the roster and the directory's other
// boards; the coordinator admits and removes boards, and each change is
// signed and published at once.

// V3NetRosterProvider is what the Board Roster screen needs from the V3Net
// service, beside V3NetStatusProvider. A provider without it offers no
// Board Roster.
type V3NetRosterProvider interface {
	// CentralRoster returns the roster the network's subscription holds,
	// or nil when it holds none.
	CentralRoster(network string) (*protocol.CentralRoster, error)
	// CentralRosterCandidates returns every verified directory listing on
	// the network, on the roster or not.
	CentralRosterCandidates(ctx context.Context, network string) ([]protocol.CentralDirectoryEntry, error)
	// PublishCentralRoster signs members as the roster, this board being
	// the coordinator, and posts it. base is the seq of the roster the edit
	// started from (0 for none); a roster changed since then is not
	// replaced, and the error says so.
	PublishCentralRoster(ctx context.Context, network string, base int64, members []protocol.CentralRosterMember) (*protocol.CentralRoster, error)
	// NodePubKeyB64 is this board's public key, which the first roster
	// always names.
	NodePubKeyB64() string
}

// rosterRow is one board on the Board Roster screen.
type rosterRow struct {
	member protocol.CentralRosterMember
	name   string // from its directory listing; "" without one
	on     bool   // on the roster
}

// rosterRows lists the roster's boards, then the directory's others, each
// group by name.
func rosterRows(roster *protocol.CentralRoster, dir []protocol.CentralDirectoryEntry) []rosterRow {
	names := map[string]string{}
	listed := map[string]protocol.CentralRosterMember{}
	for _, e := range dir {
		names[e.Listing.NodeID] = e.Listing.Name
		listed[e.Listing.NodeID] = protocol.CentralRosterMember{NodeID: e.Listing.NodeID, PubKeyB64: e.Listing.OriginPubKeyB64}
	}
	var on, off []rosterRow
	seen := map[string]bool{}
	if roster != nil {
		for _, m := range roster.Members {
			on = append(on, rosterRow{member: m, name: names[m.NodeID], on: true})
			seen[m.NodeID] = true
		}
	}
	for id, m := range listed {
		if !seen[id] {
			off = append(off, rosterRow{member: m, name: names[id]})
		}
	}
	byName := func(a, b rosterRow) int {
		if c := strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)); c != 0 {
			return c
		}
		return strings.Compare(a.member.NodeID, b.member.NodeID)
	}
	slices.SortFunc(on, byName)
	slices.SortFunc(off, byName)
	return append(on, off...)
}

// runV3NetRoster is the Board Roster screen for one of the coordinated
// networks, chosen first when there are several.
func runV3NetRoster(c *cmdCtx, networks []string) (string, string, error) {
	e, s, terminal, outputMode, termWidth := c.e, c.s, c.terminal, c.outputMode, c.termWidth
	rp, ok := e.V3NetStatus.(V3NetRosterProvider)
	if !ok || len(networks) == 0 {
		return "|08Board rosters are not available here.|07", "", nil
	}
	network := networks[0]
	if len(networks) > 1 {
		line, next, err := v3netPromptLine(s, terminal, outputMode,
			fmt.Sprintf("|07  Network (%s): ", v3netText(strings.Join(networks, ", "))))
		if err != nil {
			return "", next, err
		}
		if !slices.Contains(networks, line) {
			return "|04Not a network you coordinate.|07", "", nil
		}
		network = line
	}
	me := e.V3NetStatus.NodeID()
	status := ""
	for {
		roster, err := rp.CentralRoster(network)
		if err != nil {
			return "|04" + v3netClip(err.Error(), termWidth-4) + "|07", "", nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), v3netManageTimeout)
		dir, dirErr := rp.CentralRosterCandidates(ctx, network)
		cancel()
		rows := rosterRows(roster, dir)

		var buf strings.Builder
		buf.WriteString(ansi.ClearScreen())
		buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|12V3Net: Board Roster|07 |08— %s|07\r\n", v3netText(network)) + v3netRule(termWidth))))
		if roster == nil {
			buf.Write(ansi.ReplacePipeCodes([]byte("|07  No roster yet: boards show every board's Central listings, activity and alerts.\r\n")))
			buf.Write(ansi.ReplacePipeCodes([]byte("|07  Once you publish one, boards that hold it show only the boards on it.|07\r\n\r\n")))
		} else {
			buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|07  %d boards on the roster. Boards that hold it show Central only from these.|07\r\n\r\n", len(roster.Members)))))
		}
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  #  ROSTER  BOARD                          NODE ID|07\r\n")))
		for i, r := range rows {
			mark := "|08  -   |07"
			if r.on {
				mark = "|10  on  |07"
			}
			name := r.name
			if name == "" {
				name = "(no listing)"
			}
			id := r.member.NodeID
			if id == me {
				id += " (this node)"
			}
			buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|15%3d|07%s %s %s\r\n", i+1, mark, v3netCell(name, 30), v3netClip(id, max(termWidth-44, 16))))))
		}
		if len(rows) == 0 {
			buf.Write(ansi.ReplacePipeCodes([]byte("|08  No boards listed in the directory yet; add one by its key with K.|07\r\n")))
		}
		if dirErr != nil {
			buf.Write(ansi.ReplacePipeCodes([]byte("|04  Directory: " + v3netClip(dirErr.Error(), termWidth-15) + "|07\r\n")))
		}
		buf.WriteString("\r\n")
		buf.Write(ansi.ReplacePipeCodes([]byte(v3netRule(termWidth))))
		if status != "" {
			buf.Write(ansi.ReplacePipeCodes([]byte("  " + status + "\r\n")))
			status = ""
		}
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

		line, next, err := v3netPromptLine(s, terminal, outputMode,
			"|08  [|15A|08]dmit #  [|15R|08]emove #  [|15K|08] admit by key  [|15Q|08]uit: |07")
		if err != nil {
			return "", next, err
		}
		if line == "" || strings.EqualFold(line, "Q") {
			return "", "", nil
		}

		var members []protocol.CentralRosterMember
		if roster != nil {
			members = slices.Clone(roster.Members)
		}
		var changed string
		switch {
		case strings.EqualFold(line, "K"):
			key, next, err := v3netPromptLine(s, terminal, outputMode, "|07  Board's public key (base64, Enter cancels): ")
			if err != nil {
				return "", next, err
			}
			if key == "" {
				continue
			}
			m, err := protocol.CentralRosterMemberFor(strings.TrimSpace(key))
			if err != nil {
				status = "|04That is not an ed25519 public key.|07"
				continue
			}
			if slices.ContainsFunc(members, func(x protocol.CentralRosterMember) bool { return x.NodeID == m.NodeID }) {
				status = "|07That board is already on the roster.|07"
				continue
			}
			members = append(members, m)
			changed = "|10Admitted " + v3netText(m.NodeID) + ".|07"
		default:
			action, n, ok := parseListCommand(line)
			if !ok || n > len(rows) || (action != 'A' && action != 'R') {
				status = "|04Enter A or R followed by a row number, K, or Q.|07"
				continue
			}
			row := rows[n-1]
			switch {
			case action == 'A' && row.on:
				status = "|07That board is already on the roster.|07"
				continue
			case action == 'R' && !row.on:
				status = "|07That board is not on the roster.|07"
				continue
			case action == 'A':
				members = append(members, row.member)
				changed = "|10Admitted " + v3netText(rosterName(row)) + ".|07"
			default:
				members = slices.DeleteFunc(members, func(x protocol.CentralRosterMember) bool { return x.NodeID == row.member.NodeID })
				changed = "|10Removed " + v3netText(rosterName(row)) + ".|07"
			}
		}

		if roster == nil {
			// The first roster changes what every board holding it shows,
			// and cannot be taken back, only replaced: say so, and put
			// this board on it.
			buf.Reset()
			buf.Write(ansi.ReplacePipeCodes([]byte("\r\n|14  Publishing the first roster turns it on for this network:|07\r\n")))
			buf.Write(ansi.ReplacePipeCodes([]byte("|07  boards that take it show Central listings, activity and alerts only from\r\n")))
			buf.Write(ansi.ReplacePipeCodes([]byte("|07  boards on it, and keep it. You can change it later, but not withdraw it.|07\r\n")))
			terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)
			answer, next, err := v3netPromptLine(s, terminal, outputMode, "|07  Publish it (y/N)? ")
			if err != nil {
				return "", next, err
			}
			if !strings.EqualFold(answer, "Y") {
				status = "|07Nothing published.|07"
				continue
			}
			if mine, err := protocol.CentralRosterMemberFor(rp.NodePubKeyB64()); err == nil &&
				!slices.ContainsFunc(members, func(x protocol.CentralRosterMember) bool { return x.NodeID == mine.NodeID }) {
				members = append(members, mine)
			}
		}

		var base int64
		if roster != nil && roster.CoordPubKeyB64 == rp.NodePubKeyB64() {
			base = roster.Seq
		}
		ctx, cancel = context.WithTimeout(context.Background(), v3netManageTimeout)
		_, err = rp.PublishCentralRoster(ctx, network, base, members)
		cancel()
		if err != nil {
			if strings.Contains(err.Error(), "changed since") {
				status = "|04The roster changed since you opened it (another session?). Nothing published; here it is now.|07"
				continue
			}
			status = "|04" + v3netClip(err.Error(), termWidth-4) + "|07"
			continue
		}
		status = changed
	}
}

// rosterName names a row for a status line.
func rosterName(r rosterRow) string {
	if r.name != "" {
		return r.name
	}
	return r.member.NodeID
}
