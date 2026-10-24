package ftn

import "testing"

func TestEncodeFTNString(t *testing.T) {
	cases := []struct {
		name, in, chrs, want string
	}{
		{"ascii", "Hello All", "", "Hello All"},
		{"latin and box drawing", "Café ─ news", "", "Caf\x82 \xc4 news"},
		{"cp437 chrs", "Müller", "CP437 2", "M\x81ller"},
		{"unmappable", "snow ☃", "", "snow ?"},
		{"utf-8 declared", "Café", "UTF-8 4", "Café"},
		{"utf-8 declared lower case", "Café", "utf-8 4", "Café"},
		{"already cp437 bytes", "Caf\x82", "", "Caf\x82"},
	}
	for _, c := range cases {
		if got := EncodeFTNString(c.in, c.chrs); got != c.want {
			t.Errorf("%s: EncodeFTNString(%q, %q) = %q, want %q", c.name, c.in, c.chrs, got, c.want)
		}
	}
}

// Encoding is the inverse of DecodeFTNString for anything CP437 can hold.
func TestEncodeFTNStringRoundTrip(t *testing.T) {
	for _, s := range []string{"Café ─ news", "Ünïcödé ╔═╗ ½", "plain"} {
		if got := DecodeFTNString(EncodeFTNString(s, ""), ""); got != s {
			t.Errorf("round trip of %q gave %q", s, got)
		}
	}
}
