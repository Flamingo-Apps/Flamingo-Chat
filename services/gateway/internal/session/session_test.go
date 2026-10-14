package session

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestIssueAndVerifyRoundTrip(t *testing.T) {
	s := NewSigner([]byte("test-key"))
	token, err := s.Issue("acc-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	accountID, needsRefresh, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if accountID != "acc-1" {
		t.Fatalf("expected acc-1, got %s", accountID)
	}
	if needsRefresh {
		t.Fatalf("expected a freshly issued token to not need a refresh")
	}
}

func TestVerifyRejectsAWrongKey(t *testing.T) {
	a := NewSigner([]byte("key-a"))
	b := NewSigner([]byte("key-b"))

	token, err := a.Issue("acc-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, _, err := b.Verify(token); err == nil {
		t.Fatalf("expected verification with a different key to fail")
	}
}

func TestVerifyRejectsAnExpiredToken(t *testing.T) {
	s := NewSigner([]byte("test-key"))
	claims := jwt.RegisteredClaims{
		Subject:   "acc-1",
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * Lifetime)),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-Lifetime)),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, _, err := s.Verify(token); err == nil {
		t.Fatalf("expected an expired token to fail verification")
	}
}

func TestVerifyRejectsAnUnsignedToken(t *testing.T) {
	s := NewSigner([]byte("test-key"))
	claims := jwt.RegisteredClaims{Subject: "acc-1", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
	// "none" algorithm, unsigned - the classic algorithm-confusion attack
	// this library's keyFunc check exists specifically to reject.
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, _, err := s.Verify(token); err == nil {
		t.Fatalf("expected an unsigned (alg=none) token to be rejected")
	}
}

func TestVerifyFlagsAnOldTokenForRefresh(t *testing.T) {
	s := NewSigner([]byte("test-key"))
	claims := jwt.RegisteredClaims{
		Subject:   "acc-1",
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-25 * time.Hour)),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(Lifetime)),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	_, needsRefresh, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !needsRefresh {
		t.Fatalf("expected a 25-hour-old token to need a refresh")
	}
}
