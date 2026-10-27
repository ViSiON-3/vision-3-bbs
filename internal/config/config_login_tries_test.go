package config

import "testing"

// Three tries is the default, set or not; a sysop's own number is kept.
func TestLoginTries(t *testing.T) {
	for set, want := range map[int]int{0: 3, -1: 3, 1: 1, 5: 5} {
		if got := (ServerConfig{MaxLoginTries: set}).LoginTries(); got != want {
			t.Errorf("MaxLoginTries %d: LoginTries = %d, want %d", set, got, want)
		}
	}
}
