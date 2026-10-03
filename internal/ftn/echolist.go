package ftn

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// EchoArea represents a single area from a backbone.na file.
type EchoArea struct {
	Tag         string
	Description string
}

// ParseEcholist parses a backbone.na format file.
// Format: TAG<whitespace>Description (one per line).
// Lines starting with ';' are comments. Blank lines are skipped.
func ParseEcholist(r io.Reader) ([]EchoArea, error) {
	var areas []EchoArea
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		// Split into tag and description at first whitespace run.
		// Replace tabs with spaces so we can split uniformly.
		normalized := strings.ReplaceAll(line, "\t", " ")
		fields := strings.SplitN(normalized, " ", 2)
		if len(fields) == 0 {
			continue
		}

		tag := strings.TrimSpace(fields[0])
		if tag == "" {
			continue
		}

		desc := ""
		if len(fields) == 2 {
			desc = cleanAreaDescription(fields[1])
		}

		areas = append(areas, EchoArea{Tag: tag, Description: desc})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading echolist: %w", err)
	}

	return areas, nil
}

// cleanAreaDescription drops control characters from a description read out
// of a downloaded area list. The text ends up on the sysop's terminal and in
// area names shown to callers, so an ESC in it would be a terminal sequence.
func cleanAreaDescription(desc string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, desc))
}

// CleanEcholist applies network-specific cleanup rules to a parsed echolist.
// It removes areas whose tags match any exclude pattern and strips the
// titlePrefix from area descriptions.
func CleanEcholist(areas []EchoArea, excludeTags []string, titlePrefix string) []EchoArea {
	excludeSet := make(map[string]bool, len(excludeTags))
	for _, t := range excludeTags {
		excludeSet[strings.ToUpper(t)] = true
	}

	result := make([]EchoArea, 0, len(areas))
	for _, a := range areas {
		if excludeSet[strings.ToUpper(a.Tag)] {
			continue
		}
		desc := a.Description
		if titlePrefix != "" && strings.HasPrefix(desc, titlePrefix) {
			desc = strings.TrimSpace(strings.TrimPrefix(desc, titlePrefix))
		}
		result = append(result, EchoArea{Tag: a.Tag, Description: desc})
	}
	return result
}

// EcholistIsDownloadable reports whether a registry echolist_url is something
// the wizard can actually fetch. Several networks hand their .NA file out over
// the network itself rather than the web, and the registry records only that
// filename (for example "metronet.na"). Passing one of those to an HTTP client
// fails with an opaque "unsupported protocol scheme" error, so callers check
// here first and explain where the file really comes from instead.
func EcholistIsDownloadable(url string) bool {
	lower := strings.ToLower(strings.TrimSpace(url))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// DownloadEcholist fetches an echolist from a URL, parses it, and returns
// the areas. The request is bounded by the given context.
func DownloadEcholist(ctx context.Context, url string) ([]EchoArea, error) {
	data, err := downloadAreaList(ctx, url, "echolist")
	if err != nil {
		return nil, err
	}
	return ParseEcholist(strings.NewReader(string(data)))
}

// DownloadFileEchoList fetches a network's file echo list from a URL and
// parses it with ParseFileEchoList. The request is bounded by the given
// context.
func DownloadFileEchoList(ctx context.Context, url string) ([]EchoArea, error) {
	data, err := downloadAreaList(ctx, url, "file echo list")
	if err != nil {
		return nil, err
	}
	return ParseFileEchoList(strings.NewReader(string(data)))
}

// downloadAreaList fetches a .na-style list over HTTP(S). what names the list
// in errors.
func downloadAreaList(ctx context.Context, url, what string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", what, err)
	}
	defer func() { _ = resp.Body.Close() }() // read-only

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s download returned status %d", what, resp.StatusCode)
	}

	// Limit to 2MB to prevent abuse; error rather than silently truncate so we
	// never parse a half-downloaded list as if it were complete.
	const maxList = 2 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxList+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", what, err)
	}
	if len(data) > maxList {
		return nil, fmt.Errorf("%s exceeds %d-byte limit", what, maxList)
	}
	return data, nil
}
