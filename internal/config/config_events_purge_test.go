package config

import "testing"

// TestRunsMessagePurge checks that only an enabled event running v3mail's
// purge subcommand counts as enforcing message area limits.
func TestRunsMessagePurge(t *testing.T) {
	tests := []struct {
		desc string
		ev   EventConfig
		want bool
	}{
		{"enabled binary", EventConfig{Command: "{BBS_ROOT}/v3mail", Args: []string{"purge", "--all"}, Enabled: true}, true},
		{"go run", EventConfig{Command: "go", Args: []string{"run", "{BBS_ROOT}/cmd/v3mail", "purge", "--all"}, Enabled: true}, true},
		{"windows binary", EventConfig{Command: `C:\bbs\v3mail.exe`, Args: []string{"purge"}, Enabled: true}, true},
		{"disabled", EventConfig{Command: "{BBS_ROOT}/v3mail", Args: []string{"purge", "--all"}}, false},
		{"other subcommand", EventConfig{Command: "{BBS_ROOT}/v3mail", Args: []string{"pack", "--all"}, Enabled: true}, false},
		{"another program's purge", EventConfig{Command: "{BBS_ROOT}/helper", Args: []string{"users", "purge"}, Enabled: true}, false},
	}
	for _, tt := range tests {
		c := EventsConfig{Events: []EventConfig{tt.ev}}
		if got := c.RunsMessagePurge(); got != tt.want {
			t.Errorf("%s: RunsMessagePurge = %v, want %v", tt.desc, got, tt.want)
		}
	}
}
