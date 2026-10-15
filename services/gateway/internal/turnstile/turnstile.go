// Package turnstile verifies Cloudflare Turnstile tokens for guest
// signup (docs/ARCHITECTURE.md §4.4). Verified against Cloudflare's own
// docs (developers.cloudflare.com/turnstile/get-started/server-side-validation/):
// POST https://challenges.cloudflare.com/turnstile/v0/siteverify with
// secret and response (form-encoded or JSON), returning at least a
// success boolean and, on failure, error-codes.
package turnstile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const verifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// Verifier checks a Turnstile token. Disabled is used when no secret is
// configured, as in local development (docs/ARCHITECTURE.md §4.4) - it
// always passes, rather than every caller needing its own nil check.
type Verifier interface {
	Verify(ctx context.Context, token, remoteIP string) (bool, error)
}

// Disabled always reports success, turning the CAPTCHA check off.
type Disabled struct{}

func (Disabled) Verify(context.Context, string, string) (bool, error) { return true, nil }

// Cloudflare is the real Verifier, backed by Cloudflare's siteverify
// endpoint.
type Cloudflare struct {
	secret string
	client *http.Client
	// endpoint is verifyURL in production; tests point it at an
	// httptest.Server instead of the real Cloudflare API.
	endpoint string
}

func NewCloudflare(secret string) *Cloudflare {
	return &Cloudflare{secret: secret, client: &http.Client{Timeout: 5 * time.Second}, endpoint: verifyURL}
}

type verifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

func (c *Cloudflare) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	form := url.Values{"secret": {c.secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false, fmt.Errorf("turnstile: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("turnstile: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result verifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("turnstile: decode response: %w", err)
	}
	return result.Success, nil
}
