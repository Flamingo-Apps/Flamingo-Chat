package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_AllowsAMatchingOrigin(t *testing.T) {
	handler := CORS([]string{"localhost:*"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Origin", "http://localhost:8000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:8000" {
		t.Fatalf("got Access-Control-Allow-Origin %q, want the matching origin echoed back", got)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected the wrapped handler to still run, got status %d", rec.Code)
	}
}

func TestCORS_DoesNotSetHeadersForANonMatchingOrigin(t *testing.T) {
	called := false
	handler := CORS([]string{"localhost:*"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin header, got %q", got)
	}
	// The browser enforces the actual block client-side from the missing
	// header - this middleware's job is only to not add permission for
	// an origin that doesn't match, not to reject the request itself
	// (a non-browser caller is unaffected by CORS either way).
	if !called {
		t.Fatal("expected the wrapped handler to still run for a non-browser-enforced caller")
	}
}

func TestCORS_RespondsToPreflightWithoutCallingNext(t *testing.T) {
	called := false
	handler := CORS([]string{"localhost:*"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "/v1/me", nil)
	req.Header.Set("Origin", "http://localhost:8000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("expected the preflight to be handled without reaching the wrapped handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("got status %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("expected Access-Control-Allow-Methods to be set on the preflight response")
	}
}

func TestCORS_PassesThroughARequestWithNoOriginHeader(t *testing.T) {
	called := false
	handler := CORS([]string{"localhost:*"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called || rec.Code != http.StatusOK {
		t.Fatalf("expected a plain (non-browser) request with no Origin header to pass through untouched")
	}
}

func TestOriginAllowed(t *testing.T) {
	patterns := []string{"localhost:*"}
	cases := []struct {
		origin string
		want   bool
	}{
		{"http://localhost:8000", true},
		{"https://localhost:3000", true},
		{"http://evil.example.com", false},
		{"http://localhost.evil.com", false},
	}
	for _, tc := range cases {
		if got := originAllowed(patterns, tc.origin); got != tc.want {
			t.Errorf("originAllowed(%q) = %v, want %v", tc.origin, got, tc.want)
		}
	}
}
