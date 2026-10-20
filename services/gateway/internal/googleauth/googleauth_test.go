package googleauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func newTestGoogle(t *testing.T, handler http.HandlerFunc) *Google {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	g := NewGoogle("client-1", "secret-1", "http://localhost:8080/v1/auth/google/callback")
	g.endpoint = srv.URL
	return g
}

func TestGoogleExchangeReturnsIDTokenOnSuccess(t *testing.T) {
	g := newTestGoogle(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		want := url.Values{
			"client_id":     {"client-1"},
			"client_secret": {"secret-1"},
			"code":          {"the-code"},
			"redirect_uri":  {"http://localhost:8080/v1/auth/google/callback"},
			"grant_type":    {"authorization_code"},
		}
		for k, v := range want {
			if r.PostForm.Get(k) != v[0] {
				t.Fatalf("expected form field %s=%q, got %q", k, v[0], r.PostForm.Get(k))
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": "the-id-token"})
	})

	idToken, err := g.Exchange(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if idToken != "the-id-token" {
		t.Fatalf("expected the-id-token, got %q", idToken)
	}
}

func TestGoogleExchangeReturnsErrorOnGoogleError(t *testing.T) {
	g := newTestGoogle(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	})

	if _, err := g.Exchange(context.Background(), "stale-code"); err == nil {
		t.Fatal("expected an error when Google's token endpoint reports one")
	}
}

func TestFakeExchangeReturnsCodeUnchanged(t *testing.T) {
	idToken, err := Fake{}.Exchange(context.Background(), "fake:sub-1:a@kiit.ac.in")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if idToken != "fake:sub-1:a@kiit.ac.in" { //nolint:gosec // G101 false positive: a fake test token, not a credential
		t.Fatalf("expected Fake to pass the code through unchanged, got %q", idToken)
	}
}
