package menu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// v3netProposalRow is one pending area proposal on a network this node
// coordinates.
type v3netProposalRow struct {
	network  string
	proposal protocol.AreaProposal
}

// runV3NetCoordinator is the coordinator panel for the networks whose NAL
// names this node as coordinator. It offers the pending proposal queue,
// where A approves a proposal (optionally with a different access mode) and
// R rejects it with an optional reason, and the area list, where C hands an
// area to a different manager node.
func runV3NetCoordinator(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil || e.V3NetStatus == nil {
		return nil, "", nil
	}
	svc := e.V3NetStatus

	ctx, cancel := context.WithTimeout(context.Background(), v3netManageTimeout)
	coordinated, _, errs := v3netRoles(ctx, svc)
	cancel()

	if len(coordinated) == 0 {
		var buf strings.Builder
		buf.WriteString(ansi.ClearScreen())
		buf.Write(ansi.ReplacePipeCodes([]byte("|12V3Net: Coordinator Panel|07\r\n" + v3netRule(termWidth) + "\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|08  This node is not the coordinator of any subscribed network.|07\r\n")))
		for _, msg := range errs {
			buf.Write(ansi.ReplacePipeCodes([]byte("|04  " + truncateStr(msg, termWidth-4) + "|07\r\n")))
		}
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)
		return v3netPause(e, s, terminal, outputMode, termWidth, termHeight)
	}

	status := ""
	for {
		ctx, cancel := context.WithTimeout(context.Background(), v3netManageTimeout)
		rows, fetchErrs := v3netPendingProposals(ctx, svc, coordinated)
		cancel()

		var buf strings.Builder
		buf.WriteString(ansi.ClearScreen())
		buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|12V3Net: Coordinator Panel|07 |08— %s|07\r\n", strings.Join(coordinated, ", ")) + v3netRule(termWidth) + "\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|03  [|15P|03]ending area proposals  |08(%d)|07\r\n", len(rows)))))
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|15M|03]anage area managers|07\r\n")))
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  [|15Q|03]uit|07\r\n\r\n")))
		for _, msg := range fetchErrs {
			buf.Write(ansi.ReplacePipeCodes([]byte("|04  " + truncateStr(msg, termWidth-4) + "|07\r\n")))
		}
		buf.Write(ansi.ReplacePipeCodes([]byte(v3netRule(termWidth))))
		if status != "" {
			buf.Write(ansi.ReplacePipeCodes([]byte("  " + status + "\r\n")))
			status = ""
		}
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

		line, next, err := v3netPromptLine(s, terminal, outputMode, "|07  Choice: ")
		if err != nil {
			return nil, next, err
		}
		switch strings.ToUpper(line) {
		case "", "Q":
			return nil, "", nil
		case "P":
			msg, next, err := runV3NetProposalQueue(c, coordinated)
			if err != nil {
				return nil, next, err
			}
			status = msg
		case "M":
			msg, next, err := runV3NetAreaManagers(c, coordinated)
			if err != nil {
				return nil, next, err
			}
			status = msg
		default:
			status = "|04Enter P, M or Q.|07"
		}
	}
}

// v3netPendingProposals lists pending proposals across the coordinated
// networks, collecting per-network errors as display strings.
func v3netPendingProposals(ctx context.Context, svc V3NetStatusProvider, networks []string) ([]v3netProposalRow, []string) {
	var rows []v3netProposalRow
	var errs []string
	for _, net := range networks {
		props, err := svc.ListProposals(ctx, net)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", net, err))
			continue
		}
		for _, p := range props {
			rows = append(rows, v3netProposalRow{network: net, proposal: p})
		}
	}
	return rows, errs
}

// runV3NetProposalQueue shows the pending proposals and applies approve or
// reject commands until the sysop quits. It returns a status line for the
// panel to show.
func runV3NetProposalQueue(c *cmdCtx, networks []string) (string, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	outputMode := c.outputMode
	termWidth := c.termWidth
	svc := e.V3NetStatus
	status := ""

	for {
		ctx, cancel := context.WithTimeout(context.Background(), v3netManageTimeout)
		rows, errs := v3netPendingProposals(ctx, svc, networks)
		cancel()

		var buf strings.Builder
		buf.WriteString(ansi.ClearScreen())
		buf.Write(ansi.ReplacePipeCodes([]byte("|12V3Net: Pending Area Proposals|07\r\n" + v3netRule(termWidth))))
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  #  NETWORK     AREA TAG          NAME                  FROM          MODE      PROPOSED|07\r\n")))
		now := time.Now()
		for i, r := range rows {
			p := r.proposal
			line := fmt.Sprintf("|15%3d|07  %-10s  %-16s  %-20s  %-12s  %-8s  %s",
				i+1, truncateStr(r.network, 10), truncateStr(p.Tag, 16), truncateStr(p.Name, 20),
				truncateStr(p.FromBBS, 12), truncateStr(p.AccessMode, 8), v3netAgo(p.ProposedAt, now))
			buf.Write(ansi.ReplacePipeCodes([]byte(line + "\r\n")))
		}
		if len(rows) == 0 {
			buf.Write(ansi.ReplacePipeCodes([]byte("|08  No pending proposals.|07\r\n")))
		}
		for _, msg := range errs {
			buf.Write(ansi.ReplacePipeCodes([]byte("|04  " + truncateStr(msg, termWidth-4) + "|07\r\n")))
		}
		buf.WriteString("\r\n")
		buf.Write(ansi.ReplacePipeCodes([]byte(v3netRule(termWidth))))
		shown := status
		if status != "" {
			buf.Write(ansi.ReplacePipeCodes([]byte("  " + status + "\r\n")))
			status = ""
		}
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

		if len(rows) == 0 {
			// The panel redraws on return, so hand it the last decision's
			// status (or the empty-queue notice) to show there.
			if shown == "" {
				shown = "|08No pending proposals.|07"
			}
			return shown, "", nil
		}

		line, next, err := v3netPromptLine(s, terminal, outputMode,
			"|08  [|15A|08]pprove #  [|15R|08]eject #  [|15Q|08]uit: |07")
		if err != nil {
			return "", next, err
		}
		if line == "" || strings.EqualFold(line, "Q") {
			return "", "", nil
		}
		action, n, ok := parseListCommand(line)
		if !ok || n > len(rows) || (action != 'A' && action != 'R') {
			status = "|04Enter A or R followed by a row number, or Q.|07"
			continue
		}
		row := rows[n-1]
		// Prompt for the reason or access mode before starting the request
		// timeout, so a slow typist does not hand the hub call an expired
		// context.
		reason := ""
		accessMode := ""
		switch action {
		case 'R':
			reason, next, err = v3netPromptLine(s, terminal, outputMode, "|07  Reason (optional): ")
			if err != nil {
				return "", next, err
			}
		case 'A':
			line, next, err = v3netPromptLine(s, terminal, outputMode, fmt.Sprintf(
				"|07  Access mode |08[|15O|08]pen [|15A|08]pproval [|15C|08]losed|07, Enter keeps |15%s|07: ", row.proposal.AccessMode))
			if err != nil {
				return "", next, err
			}
			var ok bool
			if accessMode, ok = parseAccessModeChoice(line); !ok {
				status = "|04Enter O, A or C, or press Enter to keep the proposed mode.|07"
				continue
			}
		}
		ctx, cancel = context.WithTimeout(context.Background(), v3netManageTimeout)
		switch action {
		case 'A':
			err = svc.ApproveProposal(ctx, row.network, row.proposal.ID, protocol.ProposalApproveRequest{AccessMode: accessMode})
			if err == nil {
				status = fmt.Sprintf("|10Approved %s. The hub has added it to the NAL.|07", row.proposal.Tag)
			}
		case 'R':
			err = svc.RejectProposal(ctx, row.network, row.proposal.ID, protocol.ProposalRejectRequest{Reason: reason})
			if err == nil {
				status = fmt.Sprintf("|14Rejected %s.|07", row.proposal.Tag)
			}
		}
		cancel()
		if err != nil {
			status = "|04" + truncateStr(err.Error(), termWidth-4) + "|07"
		}
	}
}

// parseAccessModeChoice maps an access mode answer to a protocol mode. An
// empty answer is valid and means no override.
func parseAccessModeChoice(line string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return "", true
	case "o", protocol.AccessModeOpen:
		return protocol.AccessModeOpen, true
	case "a", protocol.AccessModeApproval:
		return protocol.AccessModeApproval, true
	case "c", protocol.AccessModeClosed:
		return protocol.AccessModeClosed, true
	}
	return "", false
}

// runV3NetAreaManagers lists the areas of the coordinated networks with
// their managers and reassigns one on C. The hub takes the new manager's
// key from its own registry, so the sysop only enters a node ID, and the
// node must be an active subscriber. It returns a status line for the panel.
func runV3NetAreaManagers(c *cmdCtx, networks []string) (string, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	outputMode := c.outputMode
	termWidth := c.termWidth
	svc := e.V3NetStatus
	me := svc.NodeID()
	status := ""

	for {
		var rows []v3netManagedArea
		var errs []string
		ctx, cancel := context.WithTimeout(context.Background(), v3netManageTimeout)
		for _, net := range networks {
			n, err := svc.FetchNALForNetwork(ctx, net)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %s", net, err))
				continue
			}
			if n == nil {
				errs = append(errs, fmt.Sprintf("%s: no NAL returned", net))
				continue
			}
			for _, a := range n.Areas {
				rows = append(rows, v3netManagedArea{network: net, area: a})
			}
		}
		cancel()

		var buf strings.Builder
		buf.WriteString(ansi.ClearScreen())
		buf.Write(ansi.ReplacePipeCodes([]byte("|12V3Net: Area Managers|07\r\n" + v3netRule(termWidth))))
		buf.Write(ansi.ReplacePipeCodes([]byte("|03  #  NETWORK     AREA TAG          NAME                  MANAGER|07\r\n")))
		for i, r := range rows {
			manager := r.area.ManagerNodeID
			if manager == "" {
				manager = "(none)"
			} else if manager == me {
				manager += " (this node)"
			}
			line := fmt.Sprintf("|15%3d|07  %-10s  %-16s  %-20s  %s",
				i+1, truncateStr(r.network, 10), truncateStr(r.area.Tag, 16), truncateStr(r.area.Name, 20),
				truncateStr(manager, max(termWidth-59, 8)))
			buf.Write(ansi.ReplacePipeCodes([]byte(line + "\r\n")))
		}
		if len(rows) == 0 {
			buf.Write(ansi.ReplacePipeCodes([]byte("|08  No areas in the NAL.|07\r\n")))
		}
		for _, msg := range errs {
			buf.Write(ansi.ReplacePipeCodes([]byte("|04  " + truncateStr(msg, termWidth-4) + "|07\r\n")))
		}
		buf.WriteString("\r\n")
		buf.Write(ansi.ReplacePipeCodes([]byte(v3netRule(termWidth))))
		if status != "" {
			buf.Write(ansi.ReplacePipeCodes([]byte("  " + status + "\r\n")))
			status = ""
		}
		terminalio.WriteProcessedBytes(terminal, []byte(buf.String()), outputMode)

		if len(rows) == 0 {
			return "|08No areas to manage.|07", "", nil
		}

		line, next, err := v3netPromptLine(s, terminal, outputMode,
			"|08  [|15C|08]hange manager #  [|15Q|08]uit: |07")
		if err != nil {
			return "", next, err
		}
		if line == "" || strings.EqualFold(line, "Q") {
			return "", "", nil
		}
		action, n, ok := parseListCommand(line)
		if !ok || n > len(rows) || action != 'C' {
			status = "|04Enter C followed by a row number, or Q.|07"
			continue
		}
		row := rows[n-1]
		nodeID, next, err := v3netPromptLine(s, terminal, outputMode,
			fmt.Sprintf("|07  New manager node ID for |15%s|07 (Enter cancels): ", row.area.Tag))
		if err != nil {
			return "", next, err
		}
		if nodeID == "" {
			continue
		}
		if nodeID == row.area.ManagerNodeID {
			status = fmt.Sprintf("|14%s is already the manager of %s.|07", nodeID, row.area.Tag)
			continue
		}
		ctx, cancel = context.WithTimeout(context.Background(), v3netManageTimeout)
		err = svc.SetAreaManager(ctx, row.network, row.area.Tag, nodeID)
		cancel()
		if err != nil {
			status = "|04" + truncateStr(err.Error(), termWidth-4) + "|07"
			continue
		}
		status = fmt.Sprintf("|10%s is now managed by %s.|07", row.area.Tag, nodeID)
	}
}
