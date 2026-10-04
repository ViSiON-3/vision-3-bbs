// Package reddit imports subreddit posts and comments into read-only message
// areas. It reads old Reddit pages through a logged-in Chrome on another
// machine; nothing is ever posted back.
package reddit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// Config is configs/reddit.json.
type Config struct {
	// ChromeDebugURL is the attached Chrome's debugging endpoint, normally
	// the local end of an SSH tunnel to the machine running Chrome.
	ChromeDebugURL   string `json:"chrome_debug_url"`
	PageDelaySeconds int    `json:"page_delay_seconds"`
	MaxPostsPerSync  int    `json:"max_posts_per_sync"`
	// MaxRunSeconds caps a whole run. Keep it under the scheduler event's
	// timeout: a killed run leaves a tab open in the person's Chrome.
	MaxRunSeconds int       `json:"max_run_seconds"`
	Subreddits    []Mapping `json:"subreddits"`
}

// Mapping files one subreddit into one message area.
type Mapping struct {
	Subreddit string `json:"subreddit"`
	AreaTag   string `json:"area_tag"`
	Enabled   bool   `json:"enabled"`
}

// Selectors is configs/reddit_selectors.json: every name that depends on old
// Reddit's markup, so a markup change is a file edit.
type Selectors struct {
	ListingURL       string `json:"listing_url"`
	PageOrigin       string `json:"page_origin"`
	ContentSelector  string `json:"content_selector"`
	ReadySelector    string `json:"ready_selector"`
	ThingElement     string `json:"thing_element"`
	TypeAttr         string `json:"type_attr"`
	PostType         string `json:"post_type"`
	CommentType      string `json:"comment_type"`
	PromotedAttr     string `json:"promoted_attr"`
	IDAttr           string `json:"id_attr"`
	AuthorAttr       string `json:"author_attr"`
	PermalinkAttr    string `json:"permalink_attr"`
	CommentCountAttr string `json:"comment_count_attr"`
	TimestampMsAttr  string `json:"timestamp_ms_attr"`
	URLAttr          string `json:"url_attr"`
	TitleElement     string `json:"title_element"`
	TitleClass       string `json:"title_class"`
	BodyClass        string `json:"body_class"`
	ChildClass       string `json:"child_class"`
	TimeElement      string `json:"time_element"`
	TimeAttr         string `json:"time_attr"`
}

var subredditName = regexp.MustCompile(`^[A-Za-z0-9_]{2,21}$`)

// LoadConfig reads and validates reddit.json in configDir.
func LoadConfig(configDir string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(filepath.Join(configDir, "reddit.json"))
	if err != nil {
		return cfg, fmt.Errorf("reading reddit.json: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing reddit.json: %w", err)
	}
	if cfg.PageDelaySeconds <= 0 {
		cfg.PageDelaySeconds = 20
	}
	if cfg.MaxPostsPerSync <= 0 {
		cfg.MaxPostsPerSync = 25
	}
	if cfg.MaxRunSeconds <= 0 {
		cfg.MaxRunSeconds = 540
	}
	if cfg.ChromeDebugURL == "" {
		cfg.ChromeDebugURL = "http://127.0.0.1:9222"
	}
	for i, m := range cfg.Subreddits {
		if !subredditName.MatchString(m.Subreddit) {
			return cfg, fmt.Errorf("reddit.json subreddits[%d]: %q is not a subreddit name (give it without r/)", i, m.Subreddit)
		}
		if strings.TrimSpace(m.AreaTag) == "" {
			return cfg, fmt.Errorf("reddit.json subreddits[%d] (%s): area_tag is empty", i, m.Subreddit)
		}
	}
	return cfg, nil
}

// MaxRun is the most time one run may take.
func (c Config) MaxRun() time.Duration {
	return time.Duration(c.MaxRunSeconds) * time.Second
}

// PageDelay is the pause between page loads.
func (c Config) PageDelay() time.Duration {
	return time.Duration(c.PageDelaySeconds) * time.Second
}

// LoadSelectors reads reddit_selectors.json from configDir, or the shipped
// copy when the board has none.
func LoadSelectors(configDir string) (Selectors, error) {
	var sel Selectors
	data, err := os.ReadFile(filepath.Join(configDir, "reddit_selectors.json"))
	if errors.Is(err, os.ErrNotExist) {
		data, err = configtemplates.FS.ReadFile("reddit_selectors.json")
	}
	if err != nil {
		return sel, fmt.Errorf("reading reddit_selectors.json: %w", err)
	}
	if err := json.Unmarshal(data, &sel); err != nil {
		return sel, fmt.Errorf("parsing reddit_selectors.json: %w", err)
	}
	if sel.ListingURL == "" || sel.PageOrigin == "" || sel.ContentSelector == "" || sel.ReadySelector == "" ||
		sel.ThingElement == "" || sel.TypeAttr == "" || sel.PostType == "" || sel.CommentType == "" || sel.IDAttr == "" {
		return sel, errors.New("reddit_selectors.json: listing_url, page_origin, content_selector, ready_selector, thing_element, type_attr, post_type, comment_type and id_attr are required")
	}
	return sel, nil
}

// ListingURLFor fills the subreddit into the listing URL.
func (s Selectors) ListingURLFor(sub string) string {
	return strings.ReplaceAll(s.ListingURL, "{sub}", sub)
}

// PageURL is the page to load for a post's permalink.
func (s Selectors) PageURL(permalink string) string {
	return strings.TrimRight(s.PageOrigin, "/") + permalink
}
