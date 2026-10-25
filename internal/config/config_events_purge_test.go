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
		{"chained after an event that does not exist", EventConfig{Command: "v3mail", Args: []string{"purge", "--all"}, RunAfter: "fix", Enabled: true}, false},
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
		{"--all after a positional argument", EventConfig{Command: "v3mail", Args: []string{"purge", "msgbases/general", "--all"}, Schedule: nightly, Enabled: true}, false},
		{"--all after the -- terminator", EventConfig{Command: "v3mail", Args: []string{"purge", "--", "--all"}, Schedule: nightly, Enabled: true}, false},
		{"flag values are not positional", EventConfig{Command: "v3mail", Args: []string{"purge", "--config", "{BBS_ROOT}/configs", "--all"}, Schedule: nightly, Enabled: true}, true},
		{"unknown flag makes v3mail exit", EventConfig{Command: "v3mail", Args: []string{"purge", "--all", "--bogus"}, Schedule: nightly, Enabled: true}, false},
	}
	for _, tt := range tests {
		c := EventsConfig{Events: []EventConfig{tt.ev}}
		if got := c.RunsMessagePurge(); got != tt.want {
			t.Errorf("%s: RunsMessagePurge = %v, want %v", tt.desc, got, tt.want)
		}
	}
}

// TestRunsMessagePurgeChains checks that a purge with only run_after counts
// only when its chain reaches an enabled event the scheduler starts on its
// own, with no cycle switching chaining off.
func TestRunsMessagePurgeChains(t *testing.T) {
	purge := EventConfig{ID: "purge", Command: "v3mail", Args: []string{"purge", "--all"}, RunAfter: "fix", Enabled: true}
	fix := EventConfig{ID: "fix", Command: "v3mail", Args: []string{"fix", "--all"}, Schedule: "3 2 * * *", Enabled: true}
	tests := []struct {
		desc   string
		events []EventConfig
		want   bool
	}{
		{"after a scheduled event", []EventConfig{fix, purge}, true},
		{"after a startup event", []EventConfig{{ID: "fix", RunAtStartup: true, Enabled: true}, purge}, true},
		{"two hops from a scheduled event", []EventConfig{
			{ID: "pre", Schedule: "0 2 * * *", Enabled: true},
			{ID: "fix", RunAfter: "pre", Enabled: true}, purge}, true},
		{"after a disabled event", []EventConfig{{ID: "fix", Schedule: "3 2 * * *"}, purge}, false},
		{"after a missing event", []EventConfig{purge}, false},
		{"after an event with no trigger", []EventConfig{{ID: "fix", Enabled: true}, purge}, false},
		{"a cycle elsewhere switches chaining off", []EventConfig{fix, purge,
			{ID: "a", RunAfter: "b", Enabled: true}, {ID: "b", RunAfter: "a", Enabled: true}}, false},
	}
	for _, tt := range tests {
		if got := (EventsConfig{Events: tt.events}).RunsMessagePurge(); got != tt.want {
			t.Errorf("%s: RunsMessagePurge = %v, want %v", tt.desc, got, tt.want)
		}
	}
}
