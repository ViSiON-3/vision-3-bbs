package message

import "testing"

// An area is networked by its type, or by a V3Net leaf binding it by tag
// whatever its type.
func TestIsNetworked(t *testing.T) {
	mm := &MessageManager{IsV3NetArea: func(id int) bool { return id == 3 }}
	for _, tc := range []struct {
		area *MessageArea
		want bool
	}{
		{&MessageArea{ID: 1, AreaType: "local"}, false},
		{&MessageArea{ID: 1}, false},
		{&MessageArea{ID: 1, AreaType: " Local "}, false},
		{&MessageArea{ID: 3, AreaType: "local"}, true},
		{&MessageArea{ID: 3}, true},
		{&MessageArea{ID: 1, AreaType: "echomail"}, true},
		{&MessageArea{ID: 1, AreaType: "netmail"}, true},
		{&MessageArea{ID: 1, AreaType: "qwknet"}, true},
		{&MessageArea{ID: 1, AreaType: "v3net"}, true},
		{nil, false},
	} {
		if got := mm.IsNetworked(tc.area); got != tc.want {
			t.Errorf("IsNetworked(%+v) = %v, want %v", tc.area, got, tc.want)
		}
	}
	if (&MessageManager{}).IsNetworked(&MessageArea{ID: 3, AreaType: "local"}) {
		t.Error("a local area is networked with no V3Net hook set")
	}
}

func TestStoredAsUTF8(t *testing.T) {
	for cs, want := range map[string]bool{"": true, "UTF-8": true, "utf-8": true, "UTF8": true, " UTF-8 ": true, "CP437": false, "LATIN-1": false} {
		if got := (&DisplayMessage{Charset: cs}).StoredAsUTF8(); got != want {
			t.Errorf("StoredAsUTF8 with %q = %v, want %v", cs, got, want)
		}
	}
}
