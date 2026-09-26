package menu

import "testing"

// The login new-mail prompt drops the caller into the private-mail reader; it
// must open on the first unread message, not the oldest one on file.
func TestFirstUnreadMessage(t *testing.T) {
	tests := []struct {
		name     string
		msgNums  []int
		lastRead int
		want     int
	}{
		{"nothing read", []int{2, 5, 9}, 0, 2},
		{"some read", []int{2, 5, 9}, 5, 9},
		{"pointer between messages", []int{2, 5, 9}, 6, 9},
		{"all read falls back to first", []int{2, 5, 9}, 9, 2},
		{"pointer past the end", []int{2, 5, 9}, 40, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstUnreadMessage(tt.msgNums, tt.lastRead); got != tt.want {
				t.Errorf("firstUnreadMessage(%v, %d) = %d, want %d", tt.msgNums, tt.lastRead, got, tt.want)
			}
		})
	}
}
