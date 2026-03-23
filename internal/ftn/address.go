package ftn

import (
	"fmt"
	"strconv"
	"strings"
)

// Address represents an FTN address in zone:net/node.point format.
type Address struct {
	Zone  int
	Net   int
	Node  int
	Point int
}

// String returns the canonical string representation of the address.
// Point is omitted when zero (e.g. "21:1/100" instead of "21:1/100.0").
func (a Address) String() string {
	if a.Point != 0 {
		return fmt.Sprintf("%d:%d/%d.%d", a.Zone, a.Net, a.Node, a.Point)
	}
	return fmt.Sprintf("%d:%d/%d", a.Zone, a.Net, a.Node)
}

// ParseAddress parses an FTN address string like "21:1/100" or "21:1/100.5".
func ParseAddress(s string) (Address, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Address{}, fmt.Errorf("empty address")
	}

	var a Address

	// Split on ':'
	colonParts := strings.SplitN(s, ":", 2)
	if len(colonParts) != 2 {
		return Address{}, fmt.Errorf("missing zone separator ':' in %q", s)
	}

	zone, err := strconv.Atoi(colonParts[0])
	if err != nil || zone < 1 || zone > 65535 {
		return Address{}, fmt.Errorf("invalid zone in %q", s)
	}
	a.Zone = zone

	// Split remainder on '/'
	slashParts := strings.SplitN(colonParts[1], "/", 2)
	if len(slashParts) != 2 {
		return Address{}, fmt.Errorf("missing net/node separator '/' in %q", s)
	}

	net, err := strconv.Atoi(slashParts[0])
	if err != nil || net < 0 || net > 65535 {
		return Address{}, fmt.Errorf("invalid net in %q", s)
	}
	a.Net = net

	// Node may have .point suffix
	nodeStr := slashParts[1]
	dotParts := strings.SplitN(nodeStr, ".", 2)

	node, err := strconv.Atoi(dotParts[0])
	if err != nil || node < 0 || node > 65535 {
		return Address{}, fmt.Errorf("invalid node in %q", s)
	}
	a.Node = node

	if len(dotParts) == 2 {
		point, err := strconv.Atoi(dotParts[1])
		if err != nil || point < 0 || point > 65535 {
			return Address{}, fmt.Errorf("invalid point in %q", s)
		}
		a.Point = point
	}

	return a, nil
}

// ValidateAddress checks whether s is a valid FTN address string.
func ValidateAddress(s string) error {
	_, err := ParseAddress(s)
	return err
}
