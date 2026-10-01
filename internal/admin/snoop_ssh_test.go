package admin

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	gliderssh "github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

func TestSnoopSSH(t *testing.T) {
	tap := snoop.NewTap()
	target := func(req SnoopRequest) (*snoop.Tap, SnoopHeader, error) {
		return tap, SnoopHeader{OutputMode: "utf8", Width: 80, Height: 25, Handle: "caller"}, nil
	}
	srv := NewServer(ServerConfig{Reg: &fakeRegistry{}, StartedAt: time.Now(), MaxEvents: 4, CallsToday: func() int { return -1 }})
	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()
	go srv.Run(srvCtx)
	gliderSrv := &gliderssh.Server{
		HostSigners:      []gliderssh.Signer{genEd25519Signer(t)},
		PublicKeyHandler: func(_ gliderssh.Context, _ gliderssh.PublicKey) bool { return true },
		SubsystemHandlers: map[string]gliderssh.SubsystemHandler{
			"wfc-admin": func(s gliderssh.Session) { _ = ServeRPC(srvCtx, s, srv, "sysop", nil, nil) },
			"wfc-snoop": func(s gliderssh.Session) { _ = ServeSnoop(s, "sysop", target, func(string, ...any) {}) },
		},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gliderSrv.Serve(ln) }()
	t.Cleanup(func() { _ = gliderSrv.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := DialSSHContext(ctx, SSHDialConfig{
		Addr: ln.Addr().String(), User: "sysop", Signer: genEd25519Signer(t), Insecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// A round trip lets the admin session finish its lazy open before the
	// test closes the client.
	if _, err := client.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := client.OpenSnoop(ctx, 4, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.Header.Handle != "caller" || st.Header.Width != 80 {
		t.Fatalf("header %+v", st.Header)
	}
	tap.Output([]byte("x"))
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 16)
		n, _ := st.Read(buf)
		got <- string(buf[:n])
	}()
	select {
	case s := <-got:
		if !strings.Contains(s, "x") {
			t.Fatalf("read %q", s)
		}
	case <-ctx.Done():
		t.Fatal("no output")
	}
}
