package verify

import (
	"context"
	"fmt"
	"strings"
)

// Fake accepts tokens of the form "fake:<sub>:<email>" instead of a real
// Google-signed JWT, so SignInWithGoogle can be exercised in local
// development and the end-to-end smoke test without a browser (DEC-0049).
// HD is derived from the email's own domain, matching what a real
// Workspace-managed account's token would carry.
//
// Never used outside local development: main.go only builds this when
// DEV_FAKE_GOOGLE is set, and logs a warning when it does.
type Fake struct{}

func (Fake) Verify(_ context.Context, rawIDToken string) (GoogleClaims, error) {
	const prefix = "fake:"
	const wantShape = prefix + "<sub>:<email>"

	rest, ok := strings.CutPrefix(rawIDToken, prefix)
	if !ok {
		return GoogleClaims{}, fmt.Errorf("verify: fake verifier: token must look like %q", wantShape)
	}
	sub, email, ok := strings.Cut(rest, ":")
	if !ok || sub == "" || email == "" {
		return GoogleClaims{}, fmt.Errorf("verify: fake verifier: token must look like %q", wantShape)
	}

	var hd string
	if _, domain, ok := strings.Cut(email, "@"); ok {
		hd = domain
	}
	return GoogleClaims{Sub: sub, Email: email, EmailVerified: true, HD: hd}, nil
}
