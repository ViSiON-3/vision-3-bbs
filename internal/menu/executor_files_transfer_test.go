package menu

import (
	"errors"
	"fmt"
	"testing"
)

// protocolSelectionErrorText keeps the "not configured" message for that
// failure only (#542): any other error from the prompt gets a generic one.
func TestProtocolSelectionErrorText(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errNoTransferProtocols, "Error: No transfer protocols configured on this system."},
		{fmt.Errorf("download: %w", errNoTransferProtocols), "Error: No transfer protocols configured on this system."},
		{errors.New("read: connection reset"), "Error: Could not select a transfer protocol."},
	} {
		if got := protocolSelectionErrorText(tc.err); got != tc.want {
			t.Errorf("protocolSelectionErrorText(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
