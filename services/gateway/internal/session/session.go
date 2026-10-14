// Package session issues and verifies the gateway's own session token:
// a JWT signed with HMAC-SHA256, holding only sub/iat/exp (DEC-0014,
// docs/ARCHITECTURE.md §4.3). It deliberately carries no tier or gender -
// the gateway re-reads the account from Identity on every connection, so
// a permission change takes effect on the account's next connection
// instead of needing the token itself revoked. Revocation goes through
// the banned-accounts set, not a token deny-list.
package session

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Lifetime is how long a session token is valid for, matching guest
// account retention (DEC-0026) so a guest's session never outlives the
// account it authenticates.
const Lifetime = 60 * 24 * time.Hour

// RefreshAfter is how old a token has to be before a WebSocket auth frame
// hands back a fresh one. Active users never expire: every connection
// before the token's actual expiry extends it another Lifetime.
const RefreshAfter = 24 * time.Hour

// ErrInvalid covers every way a token can fail verification - expired,
// badly signed, wrong algorithm, or missing its subject. Callers don't
// need to distinguish these; all of them mean "sign in again."
var ErrInvalid = errors.New("session: invalid token")

// Signer issues and verifies session tokens with one HMAC key.
type Signer struct {
	key []byte
}

func NewSigner(key []byte) *Signer {
	return &Signer{key: key}
}

// Issue returns a new session token for accountID.
func (s *Signer) Issue(accountID string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   accountID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(Lifetime)),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("session: issue: %w", err)
	}
	return token, nil
}

// Verify checks tokenString's signature and expiry and returns the
// account id it authenticates. needsRefresh is true once the token is
// older than RefreshAfter, telling the caller to call Issue again and
// hand the client a new one.
func (s *Signer) Verify(tokenString string) (accountID string, needsRefresh bool, err error) {
	var claims jwt.RegisteredClaims
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (any, error) {
		// Reject anything but HMAC - without this check, a token signed
		// with, say, "none" or a key the attacker controls could pass
		// parsing. This is the standard golang-jwt defense against
		// algorithm-confusion attacks, not a hypothetical concern.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return s.key, nil
	})
	if err != nil || !token.Valid {
		return "", false, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if claims.Subject == "" {
		return "", false, fmt.Errorf("%w: missing subject", ErrInvalid)
	}

	if claims.IssuedAt != nil && time.Since(claims.IssuedAt.Time) > RefreshAfter {
		needsRefresh = true
	}
	return claims.Subject, needsRefresh, nil
}
