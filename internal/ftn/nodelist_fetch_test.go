package ftn

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// zipOf builds an in-memory zip with the given name→content members.
func zipOf(t *testing.T, members map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range members {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func serveBytes(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadNodelistPlainText(t *testing.T) {
	srv := serveBytes(t, []byte(testNodelist))
	nl, err := DownloadNodelist(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DownloadNodelist: %v", err)
	}
	if len(nl.Entries) == 0 || nl.Entries[0].Keyword != "Zone" {
		t.Fatalf("unexpected parse result: %+v", nl.Entries)
	}
}

func TestDownloadNodelistZipPicksNodelistMember(t *testing.T) {
	// The day-numbered NODELIST member must win over other text members.
	body := zipOf(t, map[string]string{
		"README.TXT":   "read me, I am much longer than the nodelist member to tempt a size-based pick",
		"NODELIST.158": testNodelist,
	})
	srv := serveBytes(t, body)
	nl, err := DownloadNodelist(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DownloadNodelist: %v", err)
	}
	if len(nl.Entries) == 0 || nl.Entries[0].Address.Zone != 21 {
		t.Fatalf("wrong member extracted: %+v", nl.Entries)
	}
}

func TestDownloadNodelistZipFallsBackToLargestMember(t *testing.T) {
	// No nodelist-looking names: pick the largest member.
	body := zipOf(t, map[string]string{
		"info.doc": "tiny",
		"data.bin": testNodelist,
	})
	srv := serveBytes(t, body)
	nl, err := DownloadNodelist(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DownloadNodelist: %v", err)
	}
	if len(nl.Entries) == 0 {
		t.Fatal("expected entries from largest member")
	}
}

func TestDownloadNodelistNestedZipRejected(t *testing.T) {
	inner := zipOf(t, map[string]string{"NODELIST.001": testNodelist})
	body := zipOf(t, map[string]string{"NODELIST.ZIP": string(inner)})
	srv := serveBytes(t, body)
	if _, err := DownloadNodelist(context.Background(), srv.URL); err == nil {
		t.Fatal("want error for nested zip")
	}
}

func TestDownloadNodelistHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	if _, err := DownloadNodelist(context.Background(), srv.URL); err == nil {
		t.Fatal("want error for HTTP 404")
	}
}
