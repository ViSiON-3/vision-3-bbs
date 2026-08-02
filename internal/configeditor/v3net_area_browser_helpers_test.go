package configeditor

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
)

// timeoutErr implements net.Error with Timeout() == true, mimicking the
// error http.Client produces when its Timeout elapses.
type timeoutErr struct{}

func (timeoutErr) Error() string {
	return "context deadline exceeded (Client.Timeout exceeded while awaiting headers)"
}
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func fetchErrorFor(t *testing.T, err error) string {
	t.Helper()
	m := Model{mode: modeV3NetAreaBrowser, configs: &allConfigs{}}
	result, _ := m.handleFetchNALMsg(fetchNALMsg{err: err})
	return result.(Model).areaBrowserError
}

func TestHandleFetchNALMsg_TimeoutShowsFriendlyError(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "http://felonynet.org:8765/v3net/v1/felonynet/nal", Err: timeoutErr{}}
	got := fetchErrorFor(t, err)
	want := "Could not fetch areas: hub timed out - it may be down or unreachable"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHandleFetchNALMsg_ConnectionRefusedShowsFriendlyError(t *testing.T) {
	opErr := &net.OpError{Op: "dial", Net: "tcp",
		Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}
	err := &url.Error{Op: "Get", URL: "http://felonynet.org:8765/v3net/v1/felonynet/nal", Err: opErr}
	got := fetchErrorFor(t, err)
	want := "Could not fetch areas: connection refused - no hub at that address"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHandleFetchNALMsg_DNSErrorShowsFriendlyError(t *testing.T) {
	dnsErr := &net.DNSError{Err: "no such host", Name: "felonynet.orgg", IsNotFound: true}
	err := &url.Error{Op: "Get", URL: "http://felonynet.orgg:8765/v3net/v1/felonynet/nal",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: dnsErr}}
	got := fetchErrorFor(t, err)
	want := "Could not fetch areas: host not found - check the hub URL"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHandleFetchNALMsg_UnrecognizedErrorKeepsCause(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "http://felonynet.org:8765/v3net/v1/felonynet/nal",
		Err: errors.New("some other failure")}
	got := fetchErrorFor(t, err)
	if !strings.Contains(got, "some other failure") {
		t.Errorf("expected cause preserved, got %q", got)
	}
}

func TestHandleSubscribeAreasMsg_TimeoutShowsFriendlyError(t *testing.T) {
	m := Model{mode: modeV3NetAreaBrowser, configs: &allConfigs{}}
	err := &url.Error{Op: "Post", URL: "http://felonynet.org:8765/v3net/v1/subscribe", Err: timeoutErr{}}
	result, _ := m.handleSubscribeAreasMsg(subscribeAreasMsg{err: err})
	got := result.(Model).message
	want := "Subscribe failed: hub timed out - it may be down or unreachable"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFriendlyErrors_FitBrowserStatusRow ensures every friendly message,
// with the longest prefix, fits the area browser's 70-wide error row
// (69 usable characters) without truncation.
func TestFriendlyErrors_FitBrowserStatusRow(t *testing.T) {
	const maxLen = 69
	errs := []error{
		&url.Error{Op: "Get", URL: "http://x/nal", Err: timeoutErr{}},
		&url.Error{Op: "Get", URL: "http://x/nal", Err: &net.OpError{Op: "dial", Net: "tcp",
			Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}},
		&url.Error{Op: "Get", URL: "http://x/nal", Err: &net.OpError{Op: "dial", Net: "tcp",
			Err: &net.DNSError{Err: "no such host", Name: "x", IsNotFound: true}}},
	}
	for _, err := range errs {
		msg := hubErrorText("Could not fetch areas", err)
		if n := len([]rune(msg)); n > maxLen {
			t.Errorf("message is %d chars, exceeds %d-char row: %q", n, maxLen, msg)
		}
	}
}

func TestHandleFetchNALMsg_NonURLErrorKeepsMessage(t *testing.T) {
	got := fetchErrorFor(t, errors.New("hub returned status 404: not found"))
	if !strings.Contains(got, "hub returned status 404") {
		t.Errorf("expected message preserved, got %q", got)
	}
}
