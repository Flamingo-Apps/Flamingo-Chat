# Verifying college email via OAuth/OIDC

## The problem this solves

The PRD left the college-verification mechanism as an open question. The naive approach - a form field where the user types their college email - can't actually prove anyone owns that email; anyone could type anyone else's address. Badge verification exists specifically to gate gender-specific matching, so it needs to actually mean something.

## Why an OAuth ID token instead

When a user does "Sign in with Google" (or any OIDC-compliant provider), the provider authenticates them for real - password, 2FA, whatever they have set up - and hands back a signed JWT called an **ID token**, containing claims like `email` and `email_verified: true`. Because it's cryptographically signed by the provider, a backend can verify that signature and trust the claims inside, without ever handling the user's password itself.

## Where this splits across services

`VerifyBadgeRequest` carries a raw `id_token` string (see `proto/identity/v1/identity.proto`), not a bare email. The actual "redirect the browser to Google, get a token back" dance is the **Gateway**'s job later (it already owns "JWT"/HTTP auth per `SYSTEM_DESIGN.md`) - Identity never talks to Google for a live login, it only ever receives a token someone else already obtained and checks it.

## Why a generic OIDC library, not a Google-specific SDK

`services/identity/internal/verify/oidc.go` uses `github.com/coreos/go-oidc/v3`, a provider-agnostic OIDC client, configured with Google's issuer URL (`https://accounts.google.com`) as just a config value, not hardcoded into the logic. If the college's real mailbox turns out to be Office365 (Microsoft) rather than Google Workspace, only `OAUTH_ISSUER_URL` changes - not this code. Same "swappable" instinct as the repository pattern in [../01-go-fundamentals/01-04-interfaces-for-testability.md](../01-go-fundamentals/01-04-interfaces-for-testability.md).

## The actual verification flow

```go
// once at startup - NewProvider does a network fetch of the issuer's
// {issuer}/.well-known/openid-configuration document
provider, _ := oidc.NewProvider(ctx, issuerURL)
verifier := provider.Verifier(&oidc.Config{ClientID: oauthClientID})

// per request
idToken, err := verifier.Verify(ctx, rawIDToken)   // checks signature + expiry + audience
var claims struct {
    Email         string `json:"email"`
    EmailVerified bool   `json:"email_verified"`
}
idToken.Claims(&claims)
```

`ClientID` matters: it's checked against the token's `aud` (audience) claim, so a token minted for some *other* application can't be replayed against this one. This is why `NewOIDCVerifier` in this repo refuses to construct a verifier with an empty client ID at all, rather than skipping that check - see `verify.Disabled`, used instead when `COLLEGE_OAUTH_CLIENT_ID` isn't configured yet.

## What's still not done

Actually testing this end to end needs a real Google Cloud OAuth client (external account setup) and the Gateway's redirect flow (a separate, later piece of work). Until then, `VerifyBadge`'s logic is proven correct via `server_test.go`'s `fakeVerifier`, which returns canned responses instead of calling a real provider.
