package ftn

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// maxNodelistBytes caps nodelist downloads and zip members. The FidoNet
// nodelist is ~3MB of text; 16MB leaves generous headroom.
const maxNodelistBytes = 16 * 1024 * 1024

// dayExtRe matches day-of-year nodelist extensions like NODELIST.158.
var dayExtRe = regexp.MustCompile(`\.\d{1,3}$`)

// DownloadNodelist fetches a nodelist from url and parses it. Zip payloads
// (detected by magic bytes, not URL extension) are unwrapped, extracting
// the member that looks most like a nodelist.
func DownloadNodelist(ctx context.Context, url string) (*Nodelist, error) {
	client := &http.Client{Timeout: 60 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching nodelist: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // read-only

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nodelist download returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxNodelistBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading nodelist: %w", err)
	}
	if len(data) > maxNodelistBytes {
		return nil, fmt.Errorf("nodelist exceeds %d-byte limit", maxNodelistBytes)
	}

	if isZipData(data) {
		data, err = extractNodelistMember(data)
		if err != nil {
			return nil, err
		}
	}
	return ParseNodelist(bytes.NewReader(data))
}

// isZipData reports whether data starts with the zip local-file magic.
func isZipData(data []byte) bool {
	return len(data) >= 4 && bytes.Equal(data[:4], []byte("PK\x03\x04"))
}

// extractNodelistMember picks and reads the most nodelist-looking member
// of a zip archive: name starting with "nodelist", then a day-numbered
// extension, then .txt, then the largest member as a last resort.
func extractNodelistMember(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening nodelist zip: %w", err)
	}

	var best *zip.File
	bestScore := -1
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || f.UncompressedSize64 > maxNodelistBytes {
			continue
		}
		score := memberScore(path.Base(f.Name))
		if score > bestScore ||
			(score == bestScore && best != nil && f.UncompressedSize64 > best.UncompressedSize64) {
			best, bestScore = f, score
		}
	}
	if best == nil {
		return nil, fmt.Errorf("nodelist zip has no usable members")
	}

	rc, err := best.Open()
	if err != nil {
		return nil, fmt.Errorf("extracting %s: %w", best.Name, err)
	}
	defer func() { _ = rc.Close() }() // read-only

	member, err := io.ReadAll(io.LimitReader(rc, maxNodelistBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", best.Name, err)
	}
	if len(member) > maxNodelistBytes {
		return nil, fmt.Errorf("zip member %s exceeds %d-byte limit", best.Name, maxNodelistBytes)
	}
	if isZipData(member) {
		return nil, fmt.Errorf("nested zip archives are not supported")
	}
	return member, nil
}

// memberScore ranks a zip member name by nodelist likelihood.
func memberScore(name string) int {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "nodelist"):
		return 3
	case dayExtRe.MatchString(lower):
		return 2
	case strings.HasSuffix(lower, ".txt"):
		return 1
	default:
		return 0
	}
}
