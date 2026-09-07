// Package clientip preserves Meteor's hop-count client address convention.
package clientip

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// Header is overwritten at Caddy's external ingress, never accepted from clients.
const Header = "X-Wekan-Client-IP"

// Count matches the positive parseInt(value, 10) prefix used by WeKan's
// resolver. Hexadecimal prefixes parse as zero. Oversized values
// remain larger than any possible header chain and therefore select the socket.
func Count(value string) int64 {
	value = trim(value)
	if strings.HasPrefix(value, "-") {
		return 0
	}
	value = strings.TrimPrefix(value, "+")
	n := 0
	for n < len(value) {
		c := value[n]
		if c < '0' || c > '9' {
			break
		}
		n++
	}
	if n == 0 {
		return 0
	}
	count, err := strconv.ParseInt(value[:n], 10, 64)
	if err != nil {
		return math.MaxInt64
	}
	return count
}

// Resolve selects the declared number of addresses from the right, ignoring
// empty comma-separated entries. A short/missing chain uses the socket peer.
// Like the source, values need not parse as IP literals: this is a throttle key,
// not an address to connect to or an authentication decision.
func Resolve(headers http.Header, remote string, count int64) string {
	if count > 0 {
		parts := []string{}
		for _, part := range strings.Split(strings.Join(headers.Values("X-Forwarded-For"), ", "), ",") {
			if part = trim(part); part != "" {
				parts = append(parts, part)
			}
		}
		if count <= int64(len(parts)) {
			return parts[int64(len(parts))-count]
		}
	}
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	if remote == "" {
		return "unknown"
	}
	return remote
}

// ECMAScript whitespace differs from Unicode White_Space at BOM and NEL.
func trim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return (r >= 9 && r <= 13) || r == 32 || r == 0xa0 || r == 0x1680 || (r >= 0x2000 && r <= 0x200a) || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
	})
}
