package ftn

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
			desc = strings.TrimSpace(fields[1])
		}

		areas = append(areas, EchoArea{Tag: tag, Description: desc})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading echolist: %w", err)
	}

	return areas, nil
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

// DownloadEcholist fetches an echolist from a URL, parses it, and returns
// the areas. The request is bounded by the given context.
func DownloadEcholist(ctx context.Context, url string) ([]EchoArea, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching echolist: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // read-only

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("echolist download returned status %d", resp.StatusCode)
	}

	// Limit to 2MB to prevent abuse; error rather than silently truncate so we
	// never parse a half-downloaded echolist as if it were complete.
	const maxEcholist = 2 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxEcholist+1))
	if err != nil {
		return nil, fmt.Errorf("reading echolist: %w", err)
	}
	if len(data) > maxEcholist {
		return nil, fmt.Errorf("echolist exceeds %d-byte limit", maxEcholist)
	}
	return ParseEcholist(strings.NewReader(string(data)))
}
