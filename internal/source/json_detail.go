package source

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/version"
)

// JSON loads a public detail object without passing it through an HTML parser.
// It is optional on Fetcher, keeping browser and existing fixture fetchers intact.
func (f *HTTPFetcher) JSON(url string) (any, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ViSiON/3 "+version.Number)
	req.Header.Set("Accept", "application/json")
	response, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", url, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("loading %s: status %d", url, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPageBytes {
		return nil, fmt.Errorf("JSON response exceeds %d bytes", maxPageBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var document any
	if err = decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", url, err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("unexpected trailing JSON data from %s", url)
	}
	return document, nil
}
func jsonPointerParts(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("JSON Pointer must begin with /")
	}
	parts := strings.Split(path[1:], "/")
	for n, part := range parts {
		for i := 0; i < len(part); i++ {
			if part[i] == '~' {
				if i+1 >= len(part) || (part[i+1] != '0' && part[i+1] != '1') {
					return nil, fmt.Errorf("invalid JSON Pointer escape")
				}
				i++
			}
		}
		parts[n] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return parts, nil
}
func jsonValue(document any, path string) any {
	parts, err := jsonPointerParts(path)
	if err != nil {
		return nil
	}
	for _, part := range parts {
		switch node := document.(type) {
		case map[string]any:
			document = node[part]
		case []any:
			if part == "" || strings.Trim(part, "0123456789") != "" {
				return nil
			}
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) || (len(part) > 1 && part[0] == '0') {
				return nil
			}
			document = node[index]
		default:
			return nil
		}
	}
	return document
}
func extractJSONRoot(document any, d Definition) (Item, error) {
	var item Item
	values := map[string]string{}
	for name, field := range d.Thread.RootFields {
		value := ""
		switch v := jsonValue(document, field.Path).(type) {
		case string:
			value = v
		case json.Number:
			value = v.String()
		case bool:
			value = strconv.FormatBool(v)
		}
		if value == "" {
			value = field.Default
		}
		if field.Required && strings.TrimSpace(value) == "" {
			return item, fmt.Errorf("JSON detail is missing required %s", name)
		}
		if value != "" {
			value += field.Suffix
		}
		if field.SkipIf != "" && value == field.SkipIf {
			return item, fmt.Errorf("JSON detail excluded by %s", name)
		}
		if field.URL && strings.HasPrefix(value, "/") {
			origin := field.Origin
			if origin == "" {
				origin = d.PageOrigin
			}
			value = strings.TrimRight(origin, "/") + value
		}
		if name == "posted_at" {
			item.PostedAt = parseTime(value, field.Time)
			if field.Required && item.PostedAt.IsZero() {
				return item, fmt.Errorf("JSON detail has invalid required posted_at")
			}
			continue
		}
		if name == "body" && !field.HTML {
			// Original line breaks and tables survive the existing HTML-to-text
			// converter. Text resembling markup remains literal, never interpreted.
			value = strings.Map(func(r rune) rune {
				if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
					return -1
				}
				return r
			}, ansi.StripAnsi(value))
			value = "<pre>" + html.EscapeString(value) + "</pre>"
		}
		values[name] = value
	}
	item.ID = values["id"]
	item.Author = values["author"]
	item.Title = values["title"]
	item.Body = values["body"]
	item.URL = values["url"]
	item.Link = values["link"]
	item.ParentID = values["parent_id"]
	item.Marker = values["marker"]
	if item.ID == "" {
		return item, fmt.Errorf("JSON detail has no ID")
	}
	return item, nil
}
