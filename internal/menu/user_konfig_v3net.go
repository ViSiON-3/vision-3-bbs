package menu

import (
	"errors"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// v3netSharingKonfigKey is the Konfig hotkey for V3Net Sharing, the same
// screen as V3Net Central's Preferences: Konfig is where a caller looks
// for their settings, so what V3Net shares about them is there too.
const v3netSharingKonfigKey = 'R'

// v3netSharingOffered reports whether Konfig shows V3Net Sharing: only
// when some network offers Who's Online, Last Callers or network chat, so
// a board without them never sees it.
func v3netSharingOffered(e *MenuExecutor) bool {
	return len(centralOfferedNetworks(e.V3NetCentral, protocol.CentralKindPresence)) > 0 ||
		len(centralOfferedNetworks(e.V3NetCentral, protocol.CentralKindLastCallers)) > 0 ||
		len(centralChatOfferedNetworks(e.V3NetCentral)) > 0 ||
		centralAlertsOffered(e.V3NetCentral)
}

// v3netSharingValue sums up what the caller shares: Off, or which of
// Online, Calls and Chat is on. It reads the stored record, which owns consent,
// so a change made on another node shows here.
func v3netSharingValue(st *konfigState) konfigValue {
	u := st.user()
	if st.c.userManager != nil {
		if stored, ok := st.c.userManager.GetUserByID(u.ID); ok {
			u = stored
		}
	}
	var on []string
	if len(centralSharedNetworks(st.c.e.V3NetCentral, protocol.CentralKindPresence, u)) > 0 {
		on = append(on, "Online")
	}
	if len(centralSharedNetworks(st.c.e.V3NetCentral, protocol.CentralKindLastCallers, u)) > 0 {
		on = append(on, "Calls")
	}
	if u.CentralChat && !u.CentralChatBarred && len(centralChatOfferedNetworks(st.c.e.V3NetCentral)) > 0 {
		on = append(on, "Chat")
	}
	if centralAlertsShared(st.c.e.V3NetCentral, u) {
		on = append(on, "Alerts")
	}
	if len(on) == 0 {
		return st.onOffValue(false)
	}
	return konfigValue{strings.Join(on, ", "), toneOn}
}

// editV3NetSharing opens the V3Net Sharing screen, which saves and
// republishes each change itself, and redraws Konfig after. Escaping the
// screen comes back to Konfig, as Q does.
func editV3NetSharing(st *konfigState) error {
	st.redraw = true
	_, _, err := runV3NetCentralPrefs(st.c, "")
	if errors.Is(err, errInputAborted) {
		return nil
	}
	return err
}
