package user

import "testing"

func TestOfflineSignature(t *testing.T) {
	u := &User{AutoSignature: "-- Carl"}
	if got := u.OfflineSignature(); got != "-- Carl" {
		t.Errorf("OfflineSignature = %q, want the signature", got)
	}
	u.AutoSigAsk = true
	if got := u.OfflineSignature(); got != "" {
		t.Errorf("OfflineSignature with AutoSigAsk = %q, want none", got)
	}
}
