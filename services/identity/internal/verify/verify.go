// Package verify checks Google ID tokens presented by clients as proof of
// a KIIT account (DEC-0027). See docs/kb's history (now folded into
// docs/decisions.jsonl, DEC-0011): a bare user-typed email can't prove
// ownership, but an ID token signed by Google can.
package verify

import (
	"context"
	"fmt"
)

// GoogleClaims is what a verified Google ID token vouches for.
type GoogleClaims struct {
	// Sub is Google's permanent, stable identifier for the account - the
	// key verified accounts are looked up and linked by (DEC-0027), since
	// an email address can change but this does not.
	Sub           string
	Email         string
	EmailVerified bool
	// HD is the Google Workspace/Cloud organization's domain, present
	// only when the account belongs to one. This, not the email's
	// suffix, is what must be checked to restrict access to a domain -
	// see https://developers.google.com/identity/openid-connect/openid-connect
	// and INC-0006.
	HD string
}

// GoogleVerifier verifies a raw Google ID token and reports what it
// vouches for. A fake implementation backs server_test.go, and Fake (in
// this package) backs local development (DEC-0049), so business logic is
// tested and exercised without a live provider or a browser.
type GoogleVerifier interface {
	Verify(ctx context.Context, rawIDToken string) (GoogleClaims, error)
}

// Disabled rejects every token. Used when COLLEGE_OAUTH_CLIENT_ID isn't
// configured, so the service can still start (and the RPCs that don't
// need Google still work) without a working verifier.
type Disabled struct{}

func (Disabled) Verify(context.Context, string) (GoogleClaims, error) {
	return GoogleClaims{}, fmt.Errorf("verify: Google sign-in is not configured (COLLEGE_OAUTH_CLIENT_ID unset)")
}
