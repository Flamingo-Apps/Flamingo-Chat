package server

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/internal/store"
)

// fakeStore is an in-memory AccountStore, keyed by ID, with separate
// pseudonym/email indexes to reproduce the same uniqueness errors Postgres
// would report. This is what lets server logic be tested without a real
// database - see store.AccountStore's doc comment.
type fakeStore struct {
	byID        map[string]store.Account
	byPseudonym map[string]string // pseudonym -> id
	byEmail     map[string]string // email -> id
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		byID:        map[string]store.Account{},
		byPseudonym: map[string]string{},
		byEmail:     map[string]string{},
	}
}

func (f *fakeStore) Create(_ context.Context, id, pseudonym string) (store.Account, error) {
	if _, taken := f.byPseudonym[pseudonym]; taken {
		return store.Account{}, store.ErrAlreadyExists
	}
	acc := store.Account{ID: id, Pseudonym: pseudonym, CreatedAt: time.Unix(0, 0)}
	f.byID[id] = acc
	f.byPseudonym[pseudonym] = id
	return acc, nil
}

func (f *fakeStore) Get(_ context.Context, id string) (store.Account, error) {
	acc, ok := f.byID[id]
	if !ok {
		return store.Account{}, store.ErrNotFound
	}
	return acc, nil
}

func (f *fakeStore) UpdatePseudonym(_ context.Context, id, newPseudonym string) (store.Account, error) {
	acc, ok := f.byID[id]
	if !ok {
		return store.Account{}, store.ErrNotFound
	}
	if owner, taken := f.byPseudonym[newPseudonym]; taken && owner != id {
		return store.Account{}, store.ErrAlreadyExists
	}
	delete(f.byPseudonym, acc.Pseudonym)
	acc.Pseudonym = newPseudonym
	f.byPseudonym[newPseudonym] = id
	f.byID[id] = acc
	return acc, nil
}

func (f *fakeStore) SetVerified(_ context.Context, id string, gender identityv1.Gender, verifiedEmail string) (store.Account, error) {
	acc, ok := f.byID[id]
	if !ok {
		return store.Account{}, store.ErrNotFound
	}
	if owner, taken := f.byEmail[verifiedEmail]; taken && owner != id {
		return store.Account{}, store.ErrAlreadyExists
	}
	acc.BadgeVerified = true
	acc.Gender = gender
	acc.VerifiedEmail = &verifiedEmail
	f.byEmail[verifiedEmail] = id
	f.byID[id] = acc
	return acc, nil
}

// fakeVerifier returns canned (email, emailVerified) pairs per raw token,
// standing in for a real OIDC provider in tests.
type fakeVerifier struct {
	responses map[string]fakeVerifierResponse
}

type fakeVerifierResponse struct {
	email         string
	emailVerified bool
	err           error
}

func (f *fakeVerifier) Verify(_ context.Context, rawIDToken string) (string, bool, error) {
	r, ok := f.responses[rawIDToken]
	if !ok {
		return "", false, errUnknownToken
	}
	return r.email, r.emailVerified, r.err
}

var errUnknownToken = status.Error(codes.InvalidArgument, "unknown test token")

func idGen(ids ...string) IDGenerator {
	i := 0
	return func() string {
		id := ids[i]
		i++
		return id
	}
}

func TestCreateAccount(t *testing.T) {
	s := New(newFakeStore(), &fakeVerifier{}, idGen("acc-1"), "kiit.ac.in")

	acc, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if acc.Id != "acc-1" || acc.Pseudonym != "shadow_fox" || acc.BadgeVerified {
		t.Fatalf("unexpected account: %+v", acc)
	}

	if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: ""}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty pseudonym, got %v", err)
	}

	s2 := New(newFakeStore(), &fakeVerifier{}, idGen("acc-1", "acc-2"), "kiit.ac.in")
	if _, err := s2.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "dup"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := s2.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "dup"}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected AlreadyExists for duplicate pseudonym, got %v", err)
	}
}

func TestGetAccount(t *testing.T) {
	st := newFakeStore()
	s := New(st, &fakeVerifier{}, idGen("acc-1"), "kiit.ac.in")
	if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.GetAccount(context.Background(), &identityv1.GetAccountRequest{AccountId: "acc-1"}); err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if _, err := s.GetAccount(context.Background(), &identityv1.GetAccountRequest{AccountId: "missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestUpdatePseudonym(t *testing.T) {
	st := newFakeStore()
	s := New(st, &fakeVerifier{}, idGen("acc-1"), "kiit.ac.in")
	if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	acc, err := s.UpdatePseudonym(context.Background(), &identityv1.UpdatePseudonymRequest{AccountId: "acc-1", NewPseudonym: "night_owl"})
	if err != nil || acc.Pseudonym != "night_owl" {
		t.Fatalf("UpdatePseudonym: acc=%+v err=%v", acc, err)
	}

	if _, err := s.UpdatePseudonym(context.Background(), &identityv1.UpdatePseudonymRequest{AccountId: "missing", NewPseudonym: "x"}); status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
	if _, err := s.UpdatePseudonym(context.Background(), &identityv1.UpdatePseudonymRequest{AccountId: "acc-1", NewPseudonym: ""}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty pseudonym, got %v", err)
	}
}

func TestVerifyBadge(t *testing.T) {
	verifier := &fakeVerifier{responses: map[string]fakeVerifierResponse{
		"good-token":       {email: "aro@kiit.ac.in", emailVerified: true},
		"unverified-email": {email: "aro@kiit.ac.in", emailVerified: false},
		"wrong-domain":     {email: "aro@gmail.com", emailVerified: true},
		"already-used-tok": {email: "dup@kiit.ac.in", emailVerified: true},
	}}

	newServer := func() (*Server, *fakeStore) {
		st := newFakeStore()
		s := New(st, verifier, idGen("acc-1", "acc-2"), "kiit.ac.in")
		return s, st
	}

	t.Run("success", func(t *testing.T) {
		s, _ := newServer()
		if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"}); err != nil {
			t.Fatalf("create: %v", err)
		}
		acc, err := s.VerifyBadge(context.Background(), &identityv1.VerifyBadgeRequest{
			AccountId: "acc-1",
			IdToken:   "good-token",
			Gender:    identityv1.Gender_GENDER_FEMALE,
		})
		if err != nil {
			t.Fatalf("VerifyBadge: %v", err)
		}
		if !acc.BadgeVerified || acc.Gender != identityv1.Gender_GENDER_FEMALE {
			t.Fatalf("unexpected account: %+v", acc)
		}
	})

	t.Run("unspecified gender rejected", func(t *testing.T) {
		s, _ := newServer()
		if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"}); err != nil {
			t.Fatalf("create: %v", err)
		}
		_, err := s.VerifyBadge(context.Background(), &identityv1.VerifyBadgeRequest{AccountId: "acc-1", IdToken: "good-token"})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got %v", err)
		}
	})

	t.Run("unverified email rejected", func(t *testing.T) {
		s, _ := newServer()
		if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"}); err != nil {
			t.Fatalf("create: %v", err)
		}
		_, err := s.VerifyBadge(context.Background(), &identityv1.VerifyBadgeRequest{
			AccountId: "acc-1", IdToken: "unverified-email", Gender: identityv1.Gender_GENDER_MALE,
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got %v", err)
		}
	})

	t.Run("wrong domain rejected", func(t *testing.T) {
		s, _ := newServer()
		if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"}); err != nil {
			t.Fatalf("create: %v", err)
		}
		_, err := s.VerifyBadge(context.Background(), &identityv1.VerifyBadgeRequest{
			AccountId: "acc-1", IdToken: "wrong-domain", Gender: identityv1.Gender_GENDER_MALE,
		})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("expected PermissionDenied, got %v", err)
		}
	})

	t.Run("duplicate verified email rejected", func(t *testing.T) {
		s, _ := newServer()
		if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"}); err != nil {
			t.Fatalf("create acc-1: %v", err)
		}
		if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "night_owl"}); err != nil {
			t.Fatalf("create acc-2: %v", err)
		}
		if _, err := s.VerifyBadge(context.Background(), &identityv1.VerifyBadgeRequest{
			AccountId: "acc-1", IdToken: "already-used-tok", Gender: identityv1.Gender_GENDER_MALE,
		}); err != nil {
			t.Fatalf("first verify: %v", err)
		}
		_, err := s.VerifyBadge(context.Background(), &identityv1.VerifyBadgeRequest{
			AccountId: "acc-2", IdToken: "already-used-tok", Gender: identityv1.Gender_GENDER_FEMALE,
		})
		if status.Code(err) != codes.AlreadyExists {
			t.Fatalf("expected AlreadyExists, got %v", err)
		}
	})

	t.Run("account not found", func(t *testing.T) {
		s, _ := newServer()
		_, err := s.VerifyBadge(context.Background(), &identityv1.VerifyBadgeRequest{
			AccountId: "missing", IdToken: "good-token", Gender: identityv1.Gender_GENDER_MALE,
		})
		if status.Code(err) != codes.NotFound {
			t.Fatalf("expected NotFound, got %v", err)
		}
	})
}
