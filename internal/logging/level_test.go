package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"DEBUG", slog.LevelDebug, false},
		{"debug", slog.LevelDebug, false},
		{"  Info ", slog.LevelInfo, false},
		{"WARN", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"ERROR", slog.LevelError, false},
		{"", slog.LevelInfo, true},
		{"verbose", slog.LevelInfo, true},
	}
	for _, tc := range cases {
		got, err := ParseLevel(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseLevel(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
		}
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// captureDefault installs a JSON logger over buf as slog.Default for the test,
// restoring the prior default afterward.
func captureDefault(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

func TestSecurity_AttachesCategory(t *testing.T) {
	var buf bytes.Buffer
	captureDefault(t, &buf)

	Security("login failed", "user", "alice")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v (%q)", err, buf.String())
	}
	if rec["level"] != "WARN" {
		t.Errorf("Security level = %v, want WARN", rec["level"])
	}
	if rec["category"] != "security" {
		t.Errorf("category = %v, want security", rec["category"])
	}
	if rec["user"] != "alice" {
		t.Errorf("user attr = %v, want alice", rec["user"])
	}
}

func TestFatal_LogsErrorThenExits(t *testing.T) {
	var buf bytes.Buffer
	captureDefault(t, &buf)

	var gotCode int
	prevExit := osExit
	osExit = func(code int) { gotCode = code }
	t.Cleanup(func() { osExit = prevExit })

	Fatal("boom", "reason", "disk")

	if gotCode != 1 {
		t.Errorf("Fatal exit code = %d, want 1", gotCode)
	}
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v (%q)", err, buf.String())
	}
	if rec["level"] != "ERROR" {
		t.Errorf("Fatal level = %v, want ERROR", rec["level"])
	}
	if rec["msg"] != "boom" || rec["reason"] != "disk" {
		t.Errorf("Fatal record = %v, want msg=boom reason=disk", rec)
	}
}
