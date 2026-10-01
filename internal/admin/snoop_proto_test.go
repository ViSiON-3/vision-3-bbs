package admin

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

func startSnoopAudit(t *testing.T, tap *snoop.Tap, resolveErr error, audit func(string, ...any)) (*SnoopStream, error) {
	t.Helper()
	srvSide, cliSide := net.Pipe()
	resolve := func(req SnoopRequest) (*snoop.Tap, SnoopHeader, error) {
		if resolveErr != nil {
			return nil, SnoopHeader{}, resolveErr
		}
		return tap, SnoopHeader{OutputMode: "utf8", Width: 80, Height: 25, Handle: "caller"}, nil
	}
	go func() { _ = ServeSnoop(srvSide, "sysop", resolve, audit) }()
	return ClientSnoop(cliSide, SnoopRequest{NodeID: 1, ConnectedAt: time.Unix(1, 0)})
}

func startSnoop(t *testing.T, tap *snoop.Tap, resolveErr error) (*SnoopStream, error) {
	t.Helper()
	return startSnoopAudit(t, tap, resolveErr, func(string, ...any) {})
}

func TestSnoopStreamsCatchupAndLive(t *testing.T) {
	tap := snoop.NewTap()
	tap.Output([]byte("\x1b[2Jscreen"))
	st, err := startSnoop(t, tap, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.Header.Width != 80 || st.Header.Handle != "caller" {
		t.Fatalf("header %+v", st.Header)
	}
	buf := make([]byte, 64)
	n, _ := st.Read(buf)
	if !strings.Contains(string(buf[:n]), "screen") {
		t.Fatalf("catch-up %q", buf[:n])
	}
	tap.Output([]byte("live"))
	n, _ = st.Read(buf)
	if string(buf[:n]) != "live" {
		t.Fatalf("live %q", buf[:n])
	}
}

func TestSnoopRefusedForReusedNode(t *testing.T) {
	_, err := startSnoop(t, nil, errors.New("node 1 now has a different caller"))
	if err == nil || !strings.Contains(err.Error(), "different caller") {
		t.Fatalf("err = %v", err)
	}
}

func TestSnoopClientInputReachesTapOnlyWithKeyboard(t *testing.T) {
	tap := snoop.NewTap()
	st, err := startSnoop(t, tap, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, _ = st.Write([]byte("ignored"))
	time.Sleep(20 * time.Millisecond)
	select {
	case b := <-tap.Input():
		t.Fatalf("injected without keyboard: %q", b)
	default:
	}
	_ = tap.TakeKeyboard("sysop")
	_, _ = st.Write([]byte("k"))
	select {
	case b := <-tap.Input():
		if string(b) != "k" {
			t.Fatalf("input %q", b)
		}
	case <-time.After(time.Second):
		t.Fatal("no input")
	}
}

func TestSnoopCloseReleasesKeyboard(t *testing.T) {
	tap := snoop.NewTap()
	st, err := startSnoop(t, tap, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = tap.TakeKeyboard("sysop")
	st.Close()
	deadline := time.Now().Add(time.Second)
	for tap.KeyboardHolder() != "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if tap.KeyboardHolder() != "" {
		t.Fatal("keyboard still held after channel close")
	}
}

func TestSnoopEOFWhenCallerLeaves(t *testing.T) {
	tap := snoop.NewTap()
	st, err := startSnoop(t, tap, nil)
	if err != nil {
		t.Fatal(err)
	}
	tap.Close()
	_, err = io.ReadAll(st)
	if err != nil {
		t.Fatalf("ReadAll err = %v; want clean EOF", err)
	}
}

func TestSnoopEOFWhenTapAlreadyClosed(t *testing.T) {
	tap := snoop.NewTap()
	tap.Close()
	st, err := startSnoop(t, tap, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(st)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ReadAll err = %v; want clean EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not end for a closed tap")
	}
}

func TestSnoopAuditsTypeInOff(t *testing.T) {
	type call struct {
		msg  string
		args []any
	}
	var mu sync.Mutex
	var calls []call
	audit := func(msg string, args ...any) {
		mu.Lock()
		calls = append(calls, call{msg, args})
		mu.Unlock()
	}
	tap := snoop.NewTap()
	st, err := startSnoopAudit(t, tap, nil, audit)
	if err != nil {
		t.Fatal(err)
	}
	if err := tap.TakeKeyboard("sysop"); err != nil {
		t.Fatal(err)
	}
	_, _ = st.Write([]byte("abc"))
	select {
	case <-tap.Input():
	case <-time.After(time.Second):
		t.Fatal("no input")
	}
	st.Close()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(calls)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	var msgs []string
	var off *call
	for i := range calls {
		msgs = append(msgs, calls[i].msg)
		if calls[i].msg == "type-in off" {
			off = &calls[i]
		}
	}
	if off == nil {
		t.Fatalf("no type-in off audit; got %v", msgs)
	}
	if got := fmt.Sprint(off.args); !strings.Contains(got, "bytes 3") {
		t.Fatalf("type-in off args %v; want bytes 3", off.args)
	}
	if len(msgs) != 3 || msgs[1] != "type-in off" || msgs[2] != "snoop detach" {
		t.Fatalf("audit order %v", msgs)
	}
}

func TestSnoopWriteErrorEndsServe(t *testing.T) {
	tap := snoop.NewTap()
	srvSide, cliSide := net.Pipe()
	resolve := func(SnoopRequest) (*snoop.Tap, SnoopHeader, error) {
		return tap, SnoopHeader{OutputMode: "utf8"}, nil
	}
	served := make(chan error, 1)
	go func() { served <- ServeSnoop(srvSide, "sysop", resolve, func(string, ...any) {}) }()
	st, err := ClientSnoop(cliSide, SnoopRequest{NodeID: 1})
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	tap.Output([]byte("x"))
	select {
	case <-served:
	case <-time.After(time.Second):
		t.Fatal("ServeSnoop did not return after the client closed")
	}
}

func TestSnoopTakeKeyboardRightAfterOpen(t *testing.T) {
	tap := snoop.NewTap()
	st, err := startSnoop(t, tap, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := tap.TakeKeyboard("sysop"); err != nil {
		t.Fatalf("TakeKeyboard right after open: %v", err)
	}
}

func TestRefuseSnoopReadsTheRequestFirst(t *testing.T) {
	cli, srv := net.Pipe()
	defer cli.Close()
	go func() {
		defer srv.Close()
		_ = RefuseSnoop(srv, "access denied", 5*time.Second)
	}()
	// net.Pipe is unbuffered: the request write completes only if the
	// server reads it.
	_ = cli.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := ClientSnoop(cli, SnoopRequest{NodeID: 1}); err == nil || err.Error() != "access denied" {
		t.Fatalf("err = %v, want access denied", err)
	}
}

func TestRefuseSnoopAnswersASilentClient(t *testing.T) {
	cli, srv := net.Pipe()
	defer cli.Close()
	go func() {
		defer srv.Close()
		_ = RefuseSnoop(srv, "access denied", 50*time.Millisecond)
	}()
	_ = cli.SetDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(cli).ReadString('\n')
	if err != nil || !strings.Contains(line, "access denied") {
		t.Fatalf("line %q err %v", line, err)
	}
}
