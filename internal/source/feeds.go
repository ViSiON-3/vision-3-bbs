package source

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is configs/sources.json: where the debug Chrome is, how gently to
// load pages, and the feeds.
type Config struct {
	// ChromeDebugURL is the attached Chrome's debugging endpoint, normally
	// the local end of an SSH tunnel or a tailnet address.
	ChromeDebugURL   string `json:"chrome_debug_url"`
	PageDelaySeconds int    `json:"page_delay_seconds"`
	MaxItemsPerSync  int    `json:"max_items_per_sync"`
	// MaxRunSeconds caps a whole run. Keep it under the scheduler event's
	// timeout: a killed run leaves a tab open in the sysop's Chrome.
	MaxRunSeconds      int    `json:"max_run_seconds"`
	PageTimeoutSeconds int    `json:"page_timeout_seconds"`
	Feeds              []Feed `json:"feeds"`
	// FromRedditJSON is set when the board has no sources.json and its
	// reddit.json was read in its place.
	FromRedditJSON bool `json:"-"`
}

// redditJSON is the Reddit gateway's configs/reddit.json, read on boards
// that have no sources.json yet.
type redditJSON struct {
	ChromeDebugURL   string `json:"chrome_debug_url"`
	PageDelaySeconds int    `json:"page_delay_seconds"`
	MaxPostsPerSync  int    `json:"max_posts_per_sync"`
	MaxRunSeconds    int    `json:"max_run_seconds"`
	Subreddits       []struct {
		Subreddit   string `json:"subreddit"`
		AreaTag     string `json:"area_tag"`
		Enabled     bool   `json:"enabled"`
		Comments    *bool  `json:"comments,omitempty"`
		MaxComments int    `json:"max_comments,omitempty"`
	} `json:"subreddits"`
}

// LoadConfig reads and validates sources.json in configDir, or reddit.json
// when there is no sources.json. It never writes either file.
func LoadConfig(configDir string) (Config, error) {
	var cfg Config
	file := "sources.json"
	data, err := os.ReadFile(filepath.Join(configDir, file))
	switch {
	case err == nil:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("parsing %s: %w", file, err)
		}
	case errors.Is(err, os.ErrNotExist):
		old, rerr := os.ReadFile(filepath.Join(configDir, "reddit.json"))
		if errors.Is(rerr, os.ErrNotExist) {
			return cfg, fmt.Errorf("reading %s: %w", file, err)
		}
		if rerr != nil {
			return cfg, fmt.Errorf("reading reddit.json: %w", rerr)
		}
		file = "reddit.json"
		if cfg, err = fromRedditJSON(old); err != nil {
			return cfg, err
		}
	default:
		return cfg, fmt.Errorf("reading %s: %w", file, err)
	}
	cfg.setDefaults()
	return cfg, cfg.check(configDir, file)
}

func fromRedditJSON(data []byte) (Config, error) {
	var old redditJSON
	if err := json.Unmarshal(data, &old); err != nil {
		return Config{}, fmt.Errorf("parsing reddit.json: %w", err)
	}
	cfg := Config{ChromeDebugURL: old.ChromeDebugURL, PageDelaySeconds: old.PageDelaySeconds,
		MaxItemsPerSync: old.MaxPostsPerSync, MaxRunSeconds: old.MaxRunSeconds, FromRedditJSON: true}
	for _, s := range old.Subreddits {
		cfg.Feeds = append(cfg.Feeds, Feed{Source: "reddit", Target: s.Subreddit, AreaTag: s.AreaTag,
			Enabled: s.Enabled, Replies: s.Comments, MaxReplies: s.MaxComments})
	}
	return cfg, nil
}

func (c *Config) setDefaults() {
	if c.ChromeDebugURL == "" {
		c.ChromeDebugURL = "http://127.0.0.1:9222"
	}
	if c.PageDelaySeconds <= 0 {
		c.PageDelaySeconds = 20
	}
	if c.MaxItemsPerSync <= 0 {
		c.MaxItemsPerSync = 25
	}
	if c.MaxRunSeconds <= 0 {
		c.MaxRunSeconds = 540
	}
	if c.PageTimeoutSeconds <= 0 {
		c.PageTimeoutSeconds = 60
	}
}

// check validates every feed against its source definition.
func (c Config) check(configDir, file string) error {
	for i, f := range c.Feeds {
		where := fmt.Sprintf("%s feeds[%d] (%s/%s)", file, i, f.Source, f.Target)
		if strings.TrimSpace(f.AreaTag) == "" {
			return fmt.Errorf("%s: area_tag is empty", where)
		}
		if f.MaxReplies < 0 {
			return fmt.Errorf("%s: max_replies must not be negative", where)
		}
		d, err := LoadDefinition(configDir, f.Source)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if err := d.CheckTarget(f.Target); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
	}
	return nil
}

// MaxRun is the most time one run may take.
func (c Config) MaxRun() time.Duration { return time.Duration(c.MaxRunSeconds) * time.Second }

// PageDelay is the pause between page loads.
func (c Config) PageDelay() time.Duration { return time.Duration(c.PageDelaySeconds) * time.Second }

// PageTimeout bounds one page load.
func (c Config) PageTimeout() time.Duration {
	return time.Duration(c.PageTimeoutSeconds) * time.Second
}
