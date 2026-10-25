package config

import "testing"

// TestRunsMessagePurge checks that only an event the scheduler will run,
// invoking "v3mail purge --all" for real, counts as enforcing message area
// limits.
func TestRunsMessagePurge(t *testing.T) {
	const nightly = "15 2 * * *"
	tests := []struct {
		desc string
		ev   EventConfig
		want bool
	}{
		{"scheduled binary", EventConfig{Command: "{BBS_ROOT}/v3mail", Args: []string{"purge", "--all"}, Schedule: nightly, Enabled: true}, true},
		{"go run", EventConfig{Command: "go", Args: []string{"run", "{BBS_ROOT}/cmd/v3mail", "purge", "--all"}, Schedule: nightly, Enabled: true}, true},
		{"windows binary", EventConfig{Command: `C:\bbs\v3mail.exe`, Args: []string{"purge", "-all"}, Schedule: nightly, Enabled: true}, true},
		{"chained after another event", EventConfig{Command: "v3mail", Args: []string{"purge", "--all"}, RunAfter: "fix", Enabled: true}, true},
		{"run at startup", EventConfig{Command: "v3mail", Args: []string{"purge", "--all=true"}, RunAtStartup: true, Enabled: true}, true},
		{"disabled", EventConfig{Command: "{BBS_ROOT}/v3mail", Args: []string{"purge", "--all"}, Schedule: nightly}, false},
		{"no trigger", EventConfig{Command: "{BBS_ROOT}/v3mail", Args: []string{"purge", "--all"}, Enabled: true}, false},
		{"dry run", EventConfig{Command: "v3mail", Args: []string{"purge", "--all", "--dry-run"}, Schedule: nightly, Enabled: true}, false},
		{"without --all", EventConfig{Command: "v3mail", Args: []string{"purge", "--keep", "100", "msgbases/general"}, Schedule: nightly, Enabled: true}, false},
		{"--all=false", EventConfig{Command: "v3mail", Args: []string{"purge", "--all=false"}, Schedule: nightly, Enabled: true}, false},
		{"other subcommand", EventConfig{Command: "{BBS_ROOT}/v3mail", Args: []string{"pack", "--all"}, Schedule: nightly, Enabled: true}, false},
		{"purge not the subcommand", EventConfig{Command: "v3mail", Args: []string{"stats", "purge", "--all"}, Schedule: nightly, Enabled: true}, false},
		{"another program naming v3mail", EventConfig{Command: "/bin/echo", Args: []string{"v3mail", "purge", "--all"}, Schedule: nightly, Enabled: true}, false},
		{"another program's purge", EventConfig{Command: "{BBS_ROOT}/helper", Args: []string{"users", "purge"}, Schedule: nightly, Enabled: true}, false},
	}
	for _, tt := range tests {
		c := EventsConfig{Events: []EventConfig{tt.ev}}
		if got := c.RunsMessagePurge(); got != tt.want {
			t.Errorf("%s: RunsMessagePurge = %v, want %v", tt.desc, got, tt.want)
		}
	}
}
