package nntp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigDefaultsAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nntp.json")
	os.WriteFile(path, []byte(`{"server":"news.example.org:563","groups":[{"name":"comp.sys.test","area_tag":"NNTP_TEST","enabled":true}]}`), 0600)
	c, err := LoadConfig(dir)
	if err != nil || !c.TLS || c.MaxArticles != 25 || c.InitialArticles != 25 {
		t.Fatalf("config=%+v err=%v", c, err)
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Server = "news.example.org:563\r\nPOST" },
		func(c *Config) { c.Groups[0].Name = "comp.test\r\nPOST" },
		func(c *Config) { c.MaxArticles = 100000 },
		func(c *Config) { c.MaxArticleBytes = 1 << 30 },
		func(c *Config) { c.Groups = append(c.Groups, c.Groups[0]) },
	} {
		copy := c
		copy.Groups = append([]Group(nil), c.Groups...)
		mutate(&copy)
		if copy.Validate() == nil {
			t.Fatalf("accepted invalid config=%+v", copy)
		}
	}
	data, _ := json.Marshal(c)
	data = []byte(strings.Replace(string(data), `"server":`, `"typo":true,"server":`, 1))
	os.WriteFile(path, data, 0600)
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("accepted unknown config key")
	}
}
