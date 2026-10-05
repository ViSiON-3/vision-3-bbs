package util

import (
	"net"
	"strconv"
	"strings"
)

// ListenAddress joins a configured host and port for a TCP listener. It accepts
// bare or bracketed IPv6 hosts, including zone identifiers, and leaves defaults
// for empty hosts and zero ports to the caller.
func ListenAddress(host string, port int) string {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}
