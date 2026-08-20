// Package verify checks OAuth ID tokens presented by clients as proof of
// college-email ownership. See docs/kb for the reasoning: a bare
// user-typed email can't prove ownership, but an ID token signed by an
// identity provider (with a verified email_verified claim) can.
package verify

import (
	"context"
	"fmt"
)

// BadgeVerifier verifies a raw OAuth ID token and reports the email it
// vouches for. A fake implementation backs server_test.go so business
// logic (domain-suffix checks, gender requirements) is tested without a
// live provider or real credentials.
type BadgeVerifier interface {
	Verify(ctx context.Context, rawIDToken string) (email string, emailVerified bool, err error)
}

// Disabled rejects every token. Used when COLLEGE_OAUTH_CLIENT_ID isn't
// configured, so the service can still start (and CreateAccount/GetAccount/
// UpdatePseudonym still work) without a working verifier.
type Disabled struct{}

func (Disabled) Verify(context.Context, string) (string, bool, error) {
	return "", false, fmt.Errorf("verify: badge verification is not configured (COLLEGE_OAUTH_CLIENT_ID unset)")
}
