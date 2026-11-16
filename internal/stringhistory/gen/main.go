// Command gen writes history.json: for each strings.json key, every string
// value it has shipped with, read from the git history of the shipped
// template and the files it was kept in before.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// templatePaths are where the shipped strings have lived.
var templatePaths = []string{
	"templates/configs/strings.json",
	"configs/strings.json.example",
	"configs/strings.json",
}

func main() {
	out := flag.String("out", "history.json", "file to write")
	flag.Parse()
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		fail(err)
	}
	root = strings.TrimSpace(root)

	values := map[string][]string{}
	add := func(data string) {
		var doc map[string]json.RawMessage
		if json.Unmarshal([]byte(data), &doc) != nil {
			return // a version that never parsed shipped nothing usable
		}
		for k, raw := range doc {
			var s string
			if json.Unmarshal(raw, &s) != nil {
				continue // numbers such as defColorN
			}
			if !slices.Contains(values[k], s) {
				values[k] = append(values[k], s)
			}
		}
	}
	for _, path := range templatePaths {
		revs, err := git("-C", root, "log", "--format=%H", "--", path)
		if err != nil {
			fail(err)
		}
		for _, rev := range strings.Fields(revs) {
			data, err := git("-C", root, "show", rev+":"+path)
			if err != nil {
				continue // the commit deleted it
			}
			add(data)
		}
	}
	// The working copy too, so a template change and its history land in
	// the same commit.
	if data, err := os.ReadFile(root + "/templates/configs/strings.json"); err == nil {
		add(string(data))
	}
	for k := range values {
		slices.Sort(values[k])
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fail(err)
	}
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "stringhistory/gen:", err)
	os.Exit(1)
}
