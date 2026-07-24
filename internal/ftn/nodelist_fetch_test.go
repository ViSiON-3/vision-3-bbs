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

func TestDownloadNodelistZipPicksZMember(t *testing.T) {
	// A .z## member (e.g. VKRADIO.z97, tqwnet.z51) must outrank a larger
	// .txt member: it scores in the same tier as a day-numbered extension.
	body := zipOf(t, map[string]string{
		"SOMENET.TXT": "a much longer plain-text member that is not a nodelist, just padding",
		"SOMENET.z12": testNodelist,
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

func TestDownloadNodelistZipTieBreakPrefersLargerPlainDayExtMember(t *testing.T) {
	// Regression guard for fsxNet: the live fsxnet.zip ships both
	// FSXNET.205 (plain, day-numbered, 36751 bytes) and FSXNET.Z05 (a
	// nested zip wrapping the identical nodelist text, 13376 bytes as a
	// zip entry -- smaller because it is already compressed). Both now
	// score 2 in memberScore via dayExtRe/zExtRe respectively, so the
	// larger-wins tie-break must still choose the plain FSXNET.205 member,
	// preserving pre-existing extraction behavior. This is reproduced here
	// with a garbage payload behind the smaller nested member: if the
	// tie-break ever flipped to prefer the nested member, this test would
	// fail with zero parsed entries instead of silently passing.
	nested := zipOf(t, map[string]string{"FSXNET.205": "not a valid nodelist, just filler"})
	if len(nested) >= len(testNodelist) {
		t.Fatalf("fixture assumption violated: nested zip entry (%d bytes) must be smaller than the plain member (%d bytes) to match real-world fsxNet sizes", len(nested), len(testNodelist))
	}
	body := zipOf(t, map[string]string{
		"FSXNET.205": testNodelist,
		"FSXNET.Z05": string(nested),
	})
	srv := serveBytes(t, body)
	nl, err := DownloadNodelist(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DownloadNodelist: %v", err)
	}
	if len(nl.Entries) == 0 || nl.Entries[0].Address.Zone != 21 {
		t.Fatalf("wrong member extracted, want plain FSXNET.205 to win tie-break: %+v", nl.Entries)
	}
}

func TestDownloadNodelistZipSingleNestedZMemberUnwrapped(t *testing.T) {
	// In the wild, .z## members are themselves zip-compressed nodelists
	// (e.g. VKRADIO.z97 contains VKRADIO.097). One level of nesting must
	// be unwrapped automatically.
	inner := zipOf(t, map[string]string{"VKRADIO.097": testNodelist})
	body := zipOf(t, map[string]string{
		"VKRADIO.TXT": "plain text member, smaller and not chosen",
		"VKRADIO.z97": string(inner),
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

func TestDownloadNodelistNestedZipRejected(t *testing.T) {
	// One level of zip-in-zip is now unwrapped automatically (see the z##
	// test above), but a second level of nesting exceeds the unwrap budget
	// and must still be rejected.
	innermost := zipOf(t, map[string]string{"NODELIST.001": testNodelist})
	middle := zipOf(t, map[string]string{"NODELIST.ZIP": string(innermost)})
	body := zipOf(t, map[string]string{"OUTER.ZIP": string(middle)})
	srv := serveBytes(t, body)
	if _, err := DownloadNodelist(context.Background(), srv.URL); err == nil {
		t.Fatal("want error for doubly-nested zip")
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
