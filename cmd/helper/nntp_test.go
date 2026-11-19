package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/nntp"
)

func TestNNTPSyncCommandImportsOnce(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, e := ln.Accept()
			if e != nil {
				return
			}
			go func() {
				defer conn.Close()
				fmt.Fprint(conn, "201 ready\r\n")
				s := bufio.NewScanner(conn)
				for s.Scan() {
					switch s.Text() {
					case "MODE READER":
						fmt.Fprint(conn, "201 reader\r\n")
					case "GROUP comp.sys.test":
						fmt.Fprint(conn, "211 1 1 1 comp.sys.test\r\n")
					case "OVER 1-1":
						fmt.Fprint(conn, "224 overview\r\n1\tsubject\tfrom\tdate\t<a@example.test>\t\t100\t1\r\n.\r\n")
					case "ARTICLE 1":
						fmt.Fprint(conn, "220 article\r\nMessage-ID: <a@example.test>\r\nFrom: Pilot <pilot@example.invalid>\r\nSubject: Retro news\r\nDate: Thu, 29 Oct 2026 14:00:01 +0000\r\n\r\nbody\r\n.\r\n")
					default:
						return
					}
				}
			}()
		}
	}()
	root := t.TempDir()
	cfg := filepath.Join(root, "configs")
	data := filepath.Join(root, "data")
	os.MkdirAll(cfg, 0755)
	os.MkdirAll(data, 0755)
	settings := map[string]any{"server": ln.Addr().String(), "tls": false, "groups": []map[string]any{{"name": "comp.sys.test", "area_tag": "NNTP_TEST", "enabled": true}}}
	raw, _ := json.Marshal(settings)
	os.WriteFile(filepath.Join(cfg, "nntp.json"), raw, 0600)
	os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"boardName":"Test"}`), 0600)
	os.WriteFile(filepath.Join(cfg, "message_areas.json"), []byte(`[{"id":1,"position":1,"tag":"NNTP_TEST","area_type":"local","acs_write":"S256","base_path":"msgbases/test"}]`), 0600)
	for _, want := range []string{"1 articles", "0 articles"} {
		var out, errs bytes.Buffer
		if code := runNNTPSync(cfg, data, "", false, nntp.Dial, &out, &errs); code != 0 || !strings.Contains(out.String(), want) {
			t.Fatalf("code=%d out=%s err=%s", code, out.String(), errs.String())
		}
	}
}
func TestNNTPDisabledGroupDoesNotConnect(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "nntp.json"), []byte(`{"server":"news.example.org:563","groups":[{"name":"comp.sys.test","area_tag":"TEST","enabled":false}]}`), 0600)
	called := false
	dial := func(context.Context, nntp.Config) (*nntp.Client, error) {
		called = true
		return nil, errors.New("must not dial")
	}
	var out, errs bytes.Buffer
	if code := runNNTPSync(dir, t.TempDir(), "comp.sys.test", false, dial, &out, &errs); code != 1 || called {
		t.Fatalf("code=%d called=%v", code, called)
	}
}
