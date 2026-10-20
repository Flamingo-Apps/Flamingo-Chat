// Package googleauth implements the server-side half of Google's OAuth
// 2.0 authorization code flow (DEC-0092): gateway redirects the
// browser to Google, Google redirects back with a one-time code, and
// Exchanger turns that code into the ID token identity.SignInWithGoogle
// already knows how to verify. Verified against Google's own docs
// (developers.google.com/identity/protocols/oauth2/web-server): POST
// https://oauth2.googleapis.com/token with client_id, client_secret,
// code, redirect_uri and grant_type=authorization_code, returning (among
// other fields) id_token.
package googleauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const tokenURL = "https://oauth2.googleapis.com/token" //nolint:gosec // G101 false positive: this is a URL, not a credential

// Exchanger turns an authorization code Google just redirected back with
// into the ID token identity.SignInWithGoogle verifies. A real
// implementation makes the server-to-server call Exchange's own doc
// comment describes; Fake is used in local development and tests.
type Exchanger interface {
	Exchange(ctx context.Context, code string) (idToken string, err error)
}

// Google is the real Exchanger, backed by Google's own token endpoint.
type Google struct {
	clientID     string
	clientSecret string
	redirectURI  string
	client       *http.Client
	// endpoint is tokenURL in production; tests point it at an
	// httptest.Server instead of the real Google API - the same pattern
	// internal/turnstile already uses for this exact reason.
	endpoint string
}

func NewGoogle(clientID, clientSecret, redirectURI string) *Google {
	return &Google{
		clientID: clientID, clientSecret: clientSecret, redirectURI: redirectURI,
		client: &http.Client{Timeout: 5 * time.Second}, endpoint: tokenURL,
	}
}

type tokenResponse struct {
	IDToken string `json:"id_token"`
	Error   string `json:"error"`
}

func (g *Google) Exchange(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"client_id":     {g.clientID},
		"client_secret": {g.clientSecret},
		"code":          {code},
		"redirect_uri":  {g.redirectURI},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("googleauth: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("googleauth: exchange code: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("googleauth: decode token response: %w", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("googleauth: token endpoint: %s", out.Error)
	}
	if out.IDToken == "" {
		return "", fmt.Errorf("googleauth: token response had no id_token")
	}
	return out.IDToken, nil
}
