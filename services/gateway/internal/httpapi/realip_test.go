package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// remoteAddrSeen runs one request through RealIP and reports the
// RemoteAddr the next handler observed.
func remoteAddrSeen(t *testing.T, header string, set map[string]string) string {
	t.Helper()
	var seen string
	h := RealIP(header, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.RemoteAddr
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.7:41234" // the proxy's own address, e.g. cloudflared's pod
	for k, v := range set {
		req.Header.Set(k, v)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return seen
}

func TestRealIP_IgnoresTheHeaderWhenNoneIsConfigured(t *testing.T) {
	// Without a configured proxy, any client could send this header and
	// pick its own rate-limit identity.
	got := remoteAddrSeen(t, "", map[string]string{"CF-Connecting-IP": "203.0.113.9"})
	if got != "10.0.0.7:41234" {
		t.Fatalf("RemoteAddr = %q, want it unchanged", got)
	}
}

func TestRealIP_UsesTheConfiguredHeader(t *testing.T) {
	got := remoteAddrSeen(t, "CF-Connecting-IP", map[string]string{"CF-Connecting-IP": "203.0.113.9"})
	if got != "203.0.113.9:41234" {
		t.Fatalf("RemoteAddr = %q, want the client IP from the header", got)
	}
}

func TestRealIP_HandlesIPv6(t *testing.T) {
	got := remoteAddrSeen(t, "CF-Connecting-IP", map[string]string{"CF-Connecting-IP": "2001:db8::1"})
	if got != "[2001:db8::1]:41234" {
		t.Fatalf("RemoteAddr = %q, want a bracketed IPv6 host", got)
	}
}

func TestRealIP_IgnoresAMissingOrInvalidValue(t *testing.T) {
	for name, set := range map[string]map[string]string{
		"missing": {},
		"garbage": {"CF-Connecting-IP": "not-an-ip"},
		"empty":   {"CF-Connecting-IP": ""},
	} {
		if got := remoteAddrSeen(t, "CF-Connecting-IP", set); got != "10.0.0.7:41234" {
			t.Errorf("%s: RemoteAddr = %q, want it unchanged", name, got)
		}
	}
}

// For list headers, everything left of the last entry was supplied by
// whoever connected to the proxy, so only the rightmost entry - the one
// the trusted proxy itself appended - can be believed.
func TestRealIP_UsesTheRightmostEntryOfAList(t *testing.T) {
	got := remoteAddrSeen(t, "X-Forwarded-For", map[string]string{"X-Forwarded-For": "1.2.3.4, 198.51.100.20"})
	if got != "198.51.100.20:41234" {
		t.Fatalf("RemoteAddr = %q, want the rightmost entry", got)
	}
}
