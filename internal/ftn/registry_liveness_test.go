//go:build urlcheck

// Registry URL liveness check. Network-dependent, so it is behind the urlcheck
// build tag and never runs in the normal offline suite or CI. Run it by hand to
// find dead entries the wizard would fail on:
//
//	go test -tags urlcheck ./internal/ftn/ -run TestRegistryURLsLive -v
//
// It fetches every http(s) URL in the registry and reports anything that is not
// reachable, split into fetch-critical fields (echolist/nodelist/pack — the
// files a sysop actually downloads) and info-only fields. It logs rather than
// hard-fails, so one flaky host does not block a run; read the summary.
package ftn

import (
	"net/http"
	"testing"
	"time"
)

func TestRegistryURLsLive(t *testing.T) {
	networks, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	client := &http.Client{Timeout: 25 * time.Second}
	live := func(url string) (int, error) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("User-Agent", "vision3-registry-liveness/1")
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		return resp.StatusCode, nil
	}

	type field struct {
		name     string
		url      string
		critical bool
	}
	var deadCritical, deadInfo int
	for _, n := range networks {
		fields := []field{
			{"echolist_url", n.EcholistURL, true},
			{"nodelist_url", n.NodelistURL, true},
			{"pack_url", n.PackURL, true},
			{"info_url", n.InfoURL, false},
		}
		for _, f := range fields {
			if len(f.url) < 4 || f.url[:4] != "http" {
				continue // FTN-only lists and blanks are not web URLs
			}
			code, err := live(f.url)
			ok := err == nil && code >= 200 && code < 400
			if ok {
				continue
			}
			status := "error"
			if err == nil {
				status = http.StatusText(code)
			}
			marker := " "
			if f.critical {
				marker = "*"
				deadCritical++
			} else {
				deadInfo++
			}
			t.Logf("%s zone %d %-8s %-12s %-20s %s", marker, n.Zone, n.Name, f.name, status, f.url)
		}
	}
	t.Logf("dead: %d fetch-critical (*), %d info-only", deadCritical, deadInfo)
}
