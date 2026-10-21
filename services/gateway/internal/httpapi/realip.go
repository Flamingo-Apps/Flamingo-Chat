package httpapi

import (
	"net"
	"net/http"
	"strings"
)

// RealIP replaces r.RemoteAddr's host with the client IP a trusted proxy
// reports in header (for Cloudflare, CF-Connecting-IP), so everything
// downstream - the guest-signup rate limit in particular - sees the
// student's address instead of the proxy's (DEC-0099). Behind a proxy
// without this, every client shares the proxy's one address and one rate
// limit.
//
// An empty header is a no-op: the header is only believable when gateway
// is reachable solely through that proxy, so trusting it is a deployment
// decision, never a default - otherwise any client could choose its own
// rate-limit identity. A value that isn't an IP is ignored. For a list
// header (X-Forwarded-For) only the rightmost entry is used: that is the
// one the nearest proxy appended, while everything to its left came from
// whoever connected to that proxy.
func RealIP(header string, next http.Handler) http.Handler {
	if header == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ip := lastIP(r.Header.Get(header)); ip != "" {
			port := "0"
			if _, p, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				port = p
			}
			r.RemoteAddr = net.JoinHostPort(ip, port)
		}
		next.ServeHTTP(w, r)
	})
}

func lastIP(value string) string {
	parts := strings.Split(value, ",")
	candidate := strings.TrimSpace(parts[len(parts)-1])
	if net.ParseIP(candidate) == nil {
		return ""
	}
	return candidate
}
