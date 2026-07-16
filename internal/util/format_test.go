package util

import "testing"

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		name string
		size int64
		want string
	}{
		{"zero", 0, "0"},
		{"bytes", 1023, "1023"},
		{"exactly 1K", 1024, "1.0K"},
		{"kilobytes", 1536, "1.5K"},
		{"just under 1M", 1024*1024 - 1, "1024.0K"},
		{"exactly 1M", 1024 * 1024, "1.0M"},
		{"megabytes", 5 * 1024 * 1024, "5.0M"},
		{"exactly 1G", 1024 * 1024 * 1024, "1.0G"},
		{"gigabytes", int64(2.5 * 1024 * 1024 * 1024), "2.5G"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatFileSize(tt.size); got != tt.want {
				t.Errorf("FormatFileSize(%d) = %q, want %q", tt.size, got, tt.want)
			}
		})
	}
}
