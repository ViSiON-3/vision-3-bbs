package message

import (
	"encoding/json"
	"testing"
)

// A V3Net area subscribed by an earlier build can hold a hub's text as sent.
func TestV3NetAreaNameIsDefangedOnLoad(t *testing.T) {
	cases := []struct{ areaType, name, want string }{
		{"v3net", "Evil|CL\x1b[2J", "Evil/CL [2J"},
		{"v3net", "|12Sysop's colours|07", "|12Sysop's colours|07"},
		{"local", "Own |CLcode", "Own |CLcode"},
	}
	for _, tc := range cases {
		data, err := json.Marshal(map[string]string{"area_type": tc.areaType, "name": tc.name, "tag": "x"})
		if err != nil {
			t.Fatal(err)
		}
		var a MessageArea
		if err := json.Unmarshal(data, &a); err != nil {
			t.Fatal(err)
		}
		if a.Name != tc.want || a.Tag != "x" || a.AreaType != tc.areaType {
			t.Errorf("%s %q loaded as %+v, want name %q", tc.areaType, tc.name, a, tc.want)
		}
	}
}
