package verify

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDCVerifier verifies ID tokens against a generic OIDC issuer. It
// defaults to Google in main.go, but nothing here is Google-specific -
// if the college's real identity provider turns out to be something else
// (e.g. Microsoft, for an Office365-based college mailbox), only the
// configured issuer URL changes, not this code.
type OIDCVerifier struct {
	verifier *oidc.IDTokenVerifier
}

// NewOIDCVerifier performs OIDC discovery against issuerURL (a network
// call to {issuerURL}/.well-known/openid-configuration) and builds a
// verifier for tokens issued to clientID. Do this once at startup and
// reuse it - not per request. clientID must be non-empty: skipping the
// audience check would let a token minted for a different application be
// replayed here, so an empty clientID is a caller error, not something
// this constructor works around.
func NewOIDCVerifier(ctx context.Context, issuerURL, clientID string) (*OIDCVerifier, error) {
	if clientID == "" {
		return nil, fmt.Errorf("verify: clientID must not be empty")
	}
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("verify: oidc discovery against %s: %w", issuerURL, err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})
	return &OIDCVerifier{verifier: verifier}, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, rawIDToken string) (GoogleClaims, error) {
	idToken, err := v.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return GoogleClaims{}, fmt.Errorf("verify: %w", err)
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		HD            string `json:"hd"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return GoogleClaims{}, fmt.Errorf("verify: claims: %w", err)
	}
	// Subject is a top-level field on the token (part of the claims every
	// OIDC provider is required to set), not something to unmarshal from
	// the custom claims struct above.
	return GoogleClaims{
		Sub:           idToken.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		HD:            claims.HD,
	}, nil
}
