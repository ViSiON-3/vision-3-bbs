// Package nntp imports text articles from a public NNTP reader into local JAM
// areas. It never posts articles or acts as a peering/transit server.
package nntp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Group maps one newsgroup to the message area its articles are imported
// into.
type Group struct {
	Name    string `json:"name"`
	AreaTag string `json:"area_tag"`
	Enabled bool   `json:"enabled"`
}

// Config is nntp.json: the server, the limits on one run and the groups to
// import.
type Config struct {
	Server          string  `json:"server"`
	TLS             bool    `json:"tls"`
	MaxArticles     int     `json:"max_articles_per_sync"`
	InitialArticles int     `json:"initial_articles"`
	TimeoutSeconds  int     `json:"timeout_seconds"`
	MaxRunSeconds   int     `json:"max_run_seconds"`
	MaxArticleBytes int     `json:"max_article_bytes"`
	Groups          []Group `json:"groups"`
}

var groupName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_+\-]*(\.[A-Za-z0-9][A-Za-z0-9_+\-]*)+$`)

func validGroup(s string) bool { return len(s) <= 255 && groupName.MatchString(s) }

// LoadConfig reads and validates nntp.json in dir. Fields the file leaves
// out keep their defaults; a field it does not know is an error.
func LoadConfig(dir string) (Config, error) {
	c := Config{TLS: true, MaxArticles: 25, InitialArticles: 25, TimeoutSeconds: 20, MaxRunSeconds: 180, MaxArticleBytes: 262144}
	data, err := os.ReadFile(filepath.Join(dir, "nntp.json"))
	if err != nil {
		return c, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&c); err != nil {
		return c, fmt.Errorf("nntp.json: %w", err)
	}
	return c, c.Validate()
}

// Validate checks the server address, the limits and that no group or
// target area is listed twice.
func (c Config) Validate() error {
	host, port, err := net.SplitHostPort(c.Server)
	if err != nil || strings.TrimSpace(host) == "" || strings.ContainsAny(c.Server, "\r\n\t /\\") {
		return fmt.Errorf("nntp server must be host:port")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("invalid NNTP port")
	}
	if c.MaxArticles < 1 || c.MaxArticles > 100 || c.InitialArticles < 1 || c.InitialArticles > 100 {
		return fmt.Errorf("NNTP article limits must be 1..100")
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 60 || c.MaxRunSeconds < c.TimeoutSeconds || c.MaxRunSeconds > 600 {
		return fmt.Errorf("invalid NNTP timeout/run limits")
	}
	if c.MaxArticleBytes < 1024 || c.MaxArticleBytes > 1048576 {
		return fmt.Errorf("max_article_bytes must be 1024..1048576")
	}
	seen := map[string]bool{}
	areas := map[string]bool{}
	for _, g := range c.Groups {
		if !validGroup(g.Name) || strings.TrimSpace(g.AreaTag) == "" {
			return fmt.Errorf("invalid NNTP group or area: %q", g.Name)
		}
		tag := strings.ToUpper(g.AreaTag)
		if seen[g.Name] || areas[tag] {
			return fmt.Errorf("duplicate NNTP group or target area: %s", g.Name)
		}
		seen[g.Name] = true
		areas[tag] = true
	}
	return nil
}

// Timeout is the limit on one network operation.
func (c Config) Timeout() time.Duration { return time.Duration(c.TimeoutSeconds) * time.Second }

// MaxRun is the limit on a whole sync run.
func (c Config) MaxRun() time.Duration { return time.Duration(c.MaxRunSeconds) * time.Second }
