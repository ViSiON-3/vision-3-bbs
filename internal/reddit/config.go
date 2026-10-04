// Package reddit imports subreddit posts and comments into read-only message
// areas. It reads Reddit's rendered pages through headless Chrome; nothing is
// ever posted back.
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
	ChromePath       string    `json:"chrome_path"`
	PageDelaySeconds int       `json:"page_delay_seconds"`
	MaxPostsPerSync  int       `json:"max_posts_per_sync"`
	Subreddits       []Mapping `json:"subreddits"`
}

// Mapping files one subreddit into one message area.
type Mapping struct {
	Subreddit string `json:"subreddit"`
	AreaTag   string `json:"area_tag"`
	Enabled   bool   `json:"enabled"`
}

// Selectors is configs/reddit_selectors.json: every name that depends on
// Reddit's page markup, so a front-end change is a file edit.
type Selectors struct {
	ListingURL      string       `json:"listing_url"`
	PostElement     string       `json:"post_element"`
	PostAttrs       PostAttrs    `json:"post_attrs"`
	PostBodySlot    string       `json:"post_body_slot"`
	CommentElement  string       `json:"comment_element"`
	CommentAttrs    CommentAttrs `json:"comment_attrs"`
	CommentBodySlot string       `json:"comment_body_slot"`
	ReadySelector   string       `json:"ready_selector"`
}

// PostAttrs names the post element's attributes.
type PostAttrs struct {
	ID           string `json:"id"`
	Permalink    string `json:"permalink"`
	Title        string `json:"title"`
	Author       string `json:"author"`
	Created      string `json:"created"`
	CommentCount string `json:"comment_count"`
	ContentHref  string `json:"content_href"`
	PostType     string `json:"post_type"`
}

// CommentAttrs names the comment element's attributes.
type CommentAttrs struct {
	ID      string `json:"id"`
	Parent  string `json:"parent"`
	Author  string `json:"author"`
	Created string `json:"created"`
	Depth   string `json:"depth"`
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
	if sel.ListingURL == "" || sel.PostElement == "" || sel.CommentElement == "" ||
		sel.ReadySelector == "" || sel.PostAttrs.ID == "" || sel.CommentAttrs.ID == "" {
		return sel, errors.New("reddit_selectors.json: listing_url, post_element, comment_element, ready_selector and both id attributes are required")
	}
	return sel, nil
}

// ListingURLFor fills the subreddit into the listing URL.
func (s Selectors) ListingURLFor(sub string) string {
	return strings.ReplaceAll(s.ListingURL, "{sub}", sub)
}
