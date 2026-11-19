package source

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGCNJSONDetailsPreserveScienceAndDates(t *testing.T) {
	d, err := LoadDefinition(t.TempDir(), "gcn")
	if err != nil {
		t.Fatal(err)
	}
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/circulars":
			fmt.Fprint(w, `<main><ol><li value="102"><a href="/circulars/102">Correction to GCN 101: stellar flare</a></li><li value="101"><a href="/circulars/101">Transient candidate detected</a></li></ol></main>`)
		case "/circulars/101.json", "/circulars/102.json":
			detailCalls.Add(1)
			id := 101
			if strings.Contains(r.URL.Path, "102") {
				id = 102
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"circularId": id, "createdOn": int64(1793283317827), "submitter": "Observer <observer@example.invalid>", "body": "Candidate, not confirmed.\n\nRA < 10 degrees\n  A   B\n  1   2\n"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d.PageOrigin = server.URL
	d.ListingURL = server.URL + "/{feed}"
	f := NewHTTPFetcher(time.Second)
	defer f.Close()
	mm := newTestArea(t)
	im := newTestImporter(t, mm, f, d)
	noReplies := false
	feed := Feed{Source: "gcn", Target: "circulars", AreaTag: "REDDIT_BBS", Enabled: true, Replies: &noReplies}
	first := im.SyncFeed(feed)
	if first.Err != nil || first.Items != 2 || first.Pages != 3 {
		t.Fatalf("first=%+v", first)
	}
	// The listing is newest first on the real site; the API's own timestamp
	// and scientific text must survive regardless of listing metadata.
	one, err := mm.GetMessage(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if one.DateTime.Unix() != 1793283317 || !strings.HasPrefix(one.From, "GCN / Observer") || !strings.Contains(one.Body, "RA < 10 degrees\n  A   B\n  1   2") || !strings.Contains(one.Body, "Candidate, not confirmed.") {
		t.Fatalf("message=%+v", one)
	}
	if !strings.Contains(one.Body, server.URL+"/circulars/") {
		t.Fatal("missing canonical source link")
	}
	two, err := mm.GetMessage(1, 2)
	if err != nil || one.Subject != "Transient candidate detected" || two.Subject != "Correction to GCN 101: stellar flare" {
		t.Fatalf("candidate/correction labels changed: %q / %q; %v", one.Subject, two.Subject, err)
	}
	second := im.SyncFeed(feed)
	if second.Err != nil || second.Items != 0 || second.Pages != 1 || detailCalls.Load() != 2 {
		t.Fatalf("second=%+v detailCalls=%d", second, detailCalls.Load())
	}
}
func TestGCNRequiredFieldsAndDetailIdentity(t *testing.T) {
	d, err := LoadDefinition(t.TempDir(), "gcn")
	if err != nil {
		t.Fatal(err)
	}
	bad := map[string]any{"circularId": json.Number("101"), "createdOn": json.Number("1793283317827"), "submitter": "Observer"}
	if _, err = extractJSONRoot(bad, d); err == nil {
		t.Fatal("accepted missing scientific body")
	}
	bad["body"] = "original text"
	if _, err = extractJSONRoot(bad, d); err != nil {
		t.Fatal(err)
	}
	bad["createdOn"] = "not-a-timestamp"
	if _, err = extractJSONRoot(bad, d); err == nil {
		t.Fatal("accepted invalid required publication time")
	}
	// Reject a response for a different bulletin before writing any message.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/circulars" {
			fmt.Fprint(w, `<main><ol><li value="101"><a href="/circulars/101">Candidate</a></li></ol></main>`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"circularId": 999, "createdOn": int64(1793283317827), "submitter": "Observer", "body": "wrong circular"})
	}))
	defer server.Close()
	d.PageOrigin = server.URL
	d.ListingURL = server.URL + "/{feed}"
	mm := newTestArea(t)
	im := newTestImporter(t, mm, NewHTTPFetcher(time.Second), d)
	result := im.SyncFeed(Feed{Source: "gcn", Target: "circulars", AreaTag: "REDDIT_BBS"})
	if result.Err == nil || result.Items != 0 || mm.FindMessageByMSGID(1, "101@nasa-gcn") != 0 {
		t.Fatalf("mismatched detail result=%+v", result)
	}
}
func TestJSONPointerEscapesAndSourcePacing(t *testing.T) {
	doc := map[string]any{"a/b": map[string]any{"~key": []any{"science"}}}
	if got := jsonValue(doc, "/a~1b/~0key/0"); got != "science" {
		t.Fatal(got)
	}
	if _, err := jsonPointerParts("/bad~2escape"); err == nil {
		t.Fatal("accepted bad pointer")
	}
	d, err := LoadDefinition(t.TempDir(), "gcn")
	if err != nil {
		t.Fatal(err)
	}
	if d.PageDelay(20*time.Second) != time.Second {
		t.Fatal("GCN pacing override ignored")
	}
	reddit, err := LoadDefinition(t.TempDir(), "reddit")
	if err != nil {
		t.Fatal(err)
	}
	if reddit.PageDelay(20*time.Second) != 20*time.Second {
		t.Fatal("Reddit pacing changed")
	}
	bad := -1
	d.PageDelaySeconds = &bad
	if d.check("gcn") == nil {
		t.Fatal("accepted negative pacing")
	}
}
func TestJSONHTTPRejectsMalformedAndOversizedData(t *testing.T) {
	for _, kind := range []string{"malformed", "trailing", "oversized", "status"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "malformed":
					fmt.Fprint(w, `<html>not JSON</html>`)
				case "trailing":
					fmt.Fprint(w, `{} {}`)
				case "oversized":
					fmt.Fprint(w, strings.Repeat(" ", maxPageBytes+1))
				case "status":
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
				}
			}))
			defer server.Close()
			if _, err := NewHTTPFetcher(5 * time.Second).JSON(server.URL); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}
