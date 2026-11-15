package menuset

import "testing"

func TestBarInheritsAndHighlight(t *testing.T) {
	for name, tc := range map[string]struct {
		data     string
		inherits bool
		hi, lo   int
		ok       bool
	}{
		"stock highlight":      {"; comment\n0,0,31,7,*,*,FileHighlight\n", false, 31, 7, true},
		"first record counts":  {"0,0,79,3,a,b,One\n0,0,31,7,c,d,Two\n", false, 79, 3, true},
		"marker file":          {BarInheritFile, true, 0, 0, false},
		"marker, any case":     {"  inherit \n", true, 0, 0, false},
		"marker beats records": {"0,0,31,7,*,*,X\nINHERIT\n", true, 0, 0, false},
		"marker in a comment":  {"; INHERIT\n0,0,31,7,*,*,X\n", false, 31, 7, true},
		"empty":                {"", false, 0, 0, false},
		"comments only":        {"; nothing\n\n", false, 0, 0, false},
		"malformed record":     {"0,0,31\n", false, 0, 0, false},
		// As the BBS loads it: a record with unreadable colours is still
		// the first record, with the default colours; one with unreadable
		// coordinates is not a record.
		"bad colours default":  {"0,0,red,7,*,*,X\n0,0,14,1,*,*,Y\n", false, BarDefaultHighlight, BarDefaultRegular, true},
		"bad coordinates skip": {"x,0,31,7,*,*,X\n0,0,79,7,*,*,Y\n", false, 79, 7, true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := BarInherits([]byte(tc.data)); got != tc.inherits {
				t.Errorf("BarInherits = %v, want %v", got, tc.inherits)
			}
			hi, lo, ok := BarHighlight([]byte(tc.data))
			if hi != tc.hi || lo != tc.lo || ok != tc.ok {
				t.Errorf("BarHighlight = %d, %d, %v; want %d, %d, %v", hi, lo, ok, tc.hi, tc.lo, tc.ok)
			}
		})
	}
}
