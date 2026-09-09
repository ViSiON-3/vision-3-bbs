package menu

import "testing"

func TestAnonymousPostingAllowed(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name                 string
		userLevel, anonLevel int
		area, conf           *bool
		want                 bool
	}{
		{"below anon level", 5, 10, &yes, nil, false},
		{"area unset defaults to no", 50, 10, nil, nil, false},
		{"area explicitly no", 50, 10, &no, nil, false},
		{"area yes, conf unset", 50, 10, &yes, nil, true},
		{"area yes, conf yes", 50, 10, &yes, &yes, true},
		{"area yes, conf no forbids", 50, 10, &yes, &no, false},
		{"area no even if conf yes", 50, 10, &no, &yes, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := anonymousPostingAllowed(tc.userLevel, tc.anonLevel, tc.area, tc.conf); got != tc.want {
				t.Errorf("anonymousPostingAllowed(%d,%d,%v,%v) = %v, want %v",
					tc.userLevel, tc.anonLevel, tc.area, tc.conf, got, tc.want)
			}
		})
	}
}
