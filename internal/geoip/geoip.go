// Package geoip turns a caller's IP address into a short place label for
// the call log, read from a MaxMind-format database such as DB-IP's free
// City Lite file.
package geoip

import (
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"
)

// DB is an open place database. A nil *DB is valid and labels only
// private addresses.
type DB struct {
	reader *maxminddb.Reader
}

// Open opens the database at path.
func Open(path string) (*DB, error) {
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	return &DB{reader: r}, nil
}

// Close releases the database.
func (db *DB) Close() error {
	if db == nil {
		return nil
	}
	return db.reader.Close()
}

type record struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
}

// cgnat is 100.64.0.0/10, the shared address space carriers and
// Tailscale use; it says nothing about where a caller is.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// Label returns "MO/US" for a US address, the country code ("GB") for
// any other, "LAN" for a private, loopback or 100.64.0.0/10 address, and
// "" when the address is unparseable or not in the database.
func (db *DB) Label(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || cgnat.Contains(addr) {
		return "LAN"
	}
	if db == nil {
		return ""
	}
	var rec record
	if err := db.reader.Lookup(addr).Decode(&rec); err != nil {
		return ""
	}
	country := rec.Country.ISOCode
	if country == "US" && len(rec.Subdivisions) > 0 {
		if state, ok := usStates[rec.Subdivisions[0].Names["en"]]; ok {
			return state + "/US"
		}
	}
	return country
}

var usStates = map[string]string{
	"Alabama": "AL", "Alaska": "AK", "Arizona": "AZ", "Arkansas": "AR",
	"California": "CA", "Colorado": "CO", "Connecticut": "CT", "Delaware": "DE",
	"District of Columbia": "DC", "Florida": "FL", "Georgia": "GA", "Hawaii": "HI",
	"Idaho": "ID", "Illinois": "IL", "Indiana": "IN", "Iowa": "IA",
	"Kansas": "KS", "Kentucky": "KY", "Louisiana": "LA", "Maine": "ME",
	"Maryland": "MD", "Massachusetts": "MA", "Michigan": "MI", "Minnesota": "MN",
	"Mississippi": "MS", "Missouri": "MO", "Montana": "MT", "Nebraska": "NE",
	"Nevada": "NV", "New Hampshire": "NH", "New Jersey": "NJ", "New Mexico": "NM",
	"New York": "NY", "North Carolina": "NC", "North Dakota": "ND", "Ohio": "OH",
	"Oklahoma": "OK", "Oregon": "OR", "Pennsylvania": "PA", "Rhode Island": "RI",
	"South Carolina": "SC", "South Dakota": "SD", "Tennessee": "TN", "Texas": "TX",
	"Utah": "UT", "Vermont": "VT", "Virginia": "VA", "Washington": "WA",
	"West Virginia": "WV", "Wisconsin": "WI", "Wyoming": "WY", "Puerto Rico": "PR",
}
