package turnstile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDisabledAlwaysSucceeds(t *testing.T) {
	ok, err := Disabled{}.Verify(context.Background(), "", "")
	if err != nil || !ok {
		t.Fatalf("expected Disabled to always report success, got ok=%v err=%v", ok, err)
	}
}

// cloudflareAt points a Cloudflare verifier at a test server instead of
// the real endpoint - there's no reliable way to unit-test against the
// actual Cloudflare API (and no reason to; this is a third-party service
// we don't own, not infrastructure DEC-0048 means to cover), but the
// request Cloudflare's docs specify and the response shape it returns
// are both worth verifying.
func cloudflareAt(url, secret string) *Cloudflare {
	c := NewCloudflare(secret)
	c.endpoint = url
	return c
}

func TestVerifySendsTheDocumentedRequestShape(t *testing.T) {
	var gotSecret, gotResponse, gotRemoteIP, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotSecret = r.PostForm.Get("secret")
		gotResponse = r.PostForm.Get("response")
		gotRemoteIP = r.PostForm.Get("remoteip")
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer srv.Close()

	c := cloudflareAt(srv.URL, "test-secret")

	ok, err := c.Verify(context.Background(), "test-token", "1.2.3.4")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatalf("expected success")
	}
	if gotSecret != "test-secret" || gotResponse != "test-token" || gotRemoteIP != "1.2.3.4" {
		t.Fatalf("unexpected form fields: secret=%q response=%q remoteip=%q", gotSecret, gotResponse, gotRemoteIP)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("unexpected Content-Type: %q", gotContentType)
	}
}

func TestVerifyReportsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": false, "error-codes": ["invalid-input-response"]}`))
	}))
	defer srv.Close()

	c := cloudflareAt(srv.URL, "test-secret")

	ok, err := c.Verify(context.Background(), "bad-token", "")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Fatalf("expected success=false to be reported as failure")
	}
}
