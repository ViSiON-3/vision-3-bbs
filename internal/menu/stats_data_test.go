package menu

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestStatsRealData(t *testing.T) {
	for _, mode := range []ansi.OutputMode{ansi.OutputModeUTF8, ansi.OutputModeCP437} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			env := newMsgEnv(t)
			env.outputMode = mode
			seedPrivmail(env)
			env.markRead(privmailAreaID, "Caller", 1)
			u := env.caller
			u.NumUploads = 7
			u.UploadedBytes = 123*1024 + 17
			u.NumDownloads = 9
			u.DownloadedBytes = 456*1024 + 29
			u.MessagesPosted = 13
			env.um.AddCallRecord(user.CallRecord{UserID: 1, Handle: "Older", ConnectTime: time.Now().Add(-2 * time.Hour)})
			env.um.AddCallRecord(user.CallRecord{UserID: 1, Handle: "Recent", ConnectTime: time.Now().Add(-time.Hour)})
			env.um.AddCallRecord(user.CallRecord{UserID: 1, Handle: "Hidden", ConnectTime: time.Now().Add(-time.Minute), Invisible: true})
			r := env.runCmd("SHOWSTATS", u, "", "\r")
			if r.err != nil {
				t.Fatal(r.err)
			}
			for _, want := range []string{"Handle: Caller", "Access Level: 30", "Xfer Level: 30", "Uploads k: 123", "Uploads: 7", "Downloads: 9", "Downloads k: 456", "Posts: 13", "Waiting Mail: 1", "Last User: Recent"} {
				if !strings.Contains(r.text(), want) {
					t.Errorf("missing %q in %s", want, r.text())
				}
			}
			env.markRead(privmailAreaID, "Caller", 4)
			if got := env.e.statsWaitingMail(u); got != "0" {
				t.Errorf("read mailbox = %s", got)
			}
		})
	}
}
func TestStatsSuccessfulDownloadBytes(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, cmd := range []string{"/usr/bin/true", "/usr/bin/false"} {
			env := newMenuEnv(t)
			paths := []string{filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")}
			for _, p := range paths {
				if err := os.WriteFile(p, []byte("12345"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
				c.e.runTransferSend(c.s, c.terminal, transfer.ProtocolConfig{Name: "test", SendCmd: cmd, BatchSend: batch}, paths, nil, c.outputMode, c.nodeNumber, c.currentUser)
				return nil, "", c.userManager.UpdateUser(c.currentUser)
			}, env.caller, "", "")
			want := int64(0)
			if cmd == "/usr/bin/true" {
				want = 10
			}
			if saved := env.mustDiskUser(env.caller.ID); saved.DownloadedBytes != want {
				t.Errorf("persisted bytes=%d want=%d", saved.DownloadedBytes, want)
			}
			if env.caller.DownloadedBytes != want {
				t.Errorf("batch=%v cmd=%s bytes=%d want=%d", batch, cmd, env.caller.DownloadedBytes, want)
			}
		}
	}
}

func TestStatsPartialDownloadBytes(t *testing.T) {
	env := newMenuEnv(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "send.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncase \"$1\" in */b) exit 1;; esac\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(dir, "a"), filepath.Join(dir, "b")}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("12345"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		success, failed := c.e.runTransferSend(c.s, c.terminal, transfer.ProtocolConfig{Name: "test", SendCmd: "/bin/sh", SendArgs: []string{script, "{filePath}"}}, paths, nil, c.outputMode, c.nodeNumber, c.currentUser)
		if success != 1 || failed != 1 {
			t.Errorf("success=%d failed=%d", success, failed)
		}
		return nil, "", nil
	}, env.caller, "", "")
	if got := env.caller.DownloadedBytes; got != 5 {
		t.Errorf("bytes=%d want=5", got)
	}
}
