package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/internal/store"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/internal/verify"
)

// fakeStore is an in-memory AccountStore, keyed by ID, with separate
// pseudonym/email/sub indexes to reproduce the same uniqueness errors
// Postgres would report. This is what lets server logic be tested without
// a real database - see store.AccountStore's doc comment. It does not
// replicate Touch's one-hour write throttle - that's a database-level
// optimization, exercised by the integration tests against real Postgres,
// not something a unit test of the gRPC layer needs to assert on.
type fakeStore struct {
	byID        map[string]store.Account
	byPseudonym map[string]string // pseudonym -> id
	byEmail     map[string]string // verified email -> id
	bySub       map[string]string // google sub -> id
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		byID:        map[string]store.Account{},
		byPseudonym: map[string]string{},
		byEmail:     map[string]string{},
		bySub:       map[string]string{},
	}
}

func (f *fakeStore) Create(_ context.Context, id, pseudonym string) (store.Account, error) {
	if _, taken := f.byPseudonym[pseudonym]; taken {
		return store.Account{}, store.ErrAlreadyExists
	}
	acc := store.Account{ID: id, Pseudonym: pseudonym, CreatedAt: time.Unix(0, 0), LastSeenAt: time.Unix(0, 0)}
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

func (f *fakeStore) GetByGoogleSub(_ context.Context, sub string) (store.Account, error) {
	id, ok := f.bySub[sub]
	if !ok {
		return store.Account{}, store.ErrNotFound
	}
	return f.byID[id], nil
}

func (f *fakeStore) LinkGoogle(_ context.Context, id, sub, verifiedEmail string) (store.Account, error) {
	acc, ok := f.byID[id]
	if !ok {
		return store.Account{}, store.ErrNotFound
	}
	if owner, taken := f.bySub[sub]; taken && owner != id {
		return store.Account{}, store.ErrAlreadyExists
	}
	if owner, taken := f.byEmail[verifiedEmail]; taken && owner != id {
		return store.Account{}, store.ErrAlreadyExists
	}
	acc.GoogleSub = &sub
	acc.VerifiedEmail = &verifiedEmail
	f.bySub[sub] = id
	f.byEmail[verifiedEmail] = id
	f.byID[id] = acc
	return acc, nil
}

func (f *fakeStore) CreateVerified(_ context.Context, id, pseudonym, sub, verifiedEmail string) (store.Account, error) {
	if _, taken := f.byPseudonym[pseudonym]; taken {
		return store.Account{}, store.ErrAlreadyExists
	}
	if _, taken := f.bySub[sub]; taken {
		return store.Account{}, store.ErrAlreadyExists
	}
	if _, taken := f.byEmail[verifiedEmail]; taken {
		return store.Account{}, store.ErrAlreadyExists
	}
	acc := store.Account{
		ID: id, Pseudonym: pseudonym, GoogleSub: &sub, VerifiedEmail: &verifiedEmail,
		CreatedAt: time.Unix(0, 0), LastSeenAt: time.Unix(0, 0),
	}
	f.byID[id] = acc
	f.byPseudonym[pseudonym] = id
	f.bySub[sub] = id
	f.byEmail[verifiedEmail] = id
	return acc, nil
}

func (f *fakeStore) SetGender(_ context.Context, id string, gender identityv1.Gender) (store.Account, error) {
	acc, ok := f.byID[id]
	if !ok {
		return store.Account{}, store.ErrNotFound
	}
	if acc.GenderSetAt != nil {
		return store.Account{}, store.ErrAlreadySet
	}
	now := time.Now()
	acc.Gender = gender
	acc.GenderSetAt = &now
	f.byID[id] = acc
	return acc, nil
}

func (f *fakeStore) AdminSetGender(_ context.Context, id string, gender identityv1.Gender) (store.Account, error) {
	acc, ok := f.byID[id]
	if !ok {
		return store.Account{}, store.ErrNotFound
	}
	acc.Gender = gender
	if acc.GenderSetAt == nil {
		now := time.Now()
		acc.GenderSetAt = &now
	}
	f.byID[id] = acc
	return acc, nil
}

func (f *fakeStore) Touch(_ context.Context, id string) error {
	acc, ok := f.byID[id]
	if !ok {
		return nil // best-effort - see the doc comment on AccountStore.Touch
	}
	acc.LastSeenAt = time.Now()
	f.byID[id] = acc
	return nil
}

func (f *fakeStore) DeleteStaleGuests(_ context.Context, olderThan time.Duration) (int64, error) {
	var n int64
	cutoff := time.Now().Add(-olderThan)
	for id, acc := range f.byID {
		if acc.GoogleSub == nil && acc.LastSeenAt.Before(cutoff) {
			delete(f.byID, id)
			delete(f.byPseudonym, acc.Pseudonym)
			n++
		}
	}
	return n, nil
}

// fakeVerifier returns canned GoogleClaims per raw token, standing in for
// a real OIDC provider in tests.
type fakeVerifier struct {
	responses map[string]fakeVerifierResponse
}

type fakeVerifierResponse struct {
	claims verify.GoogleClaims
	err    error
}

func (f *fakeVerifier) Verify(_ context.Context, rawIDToken string) (verify.GoogleClaims, error) {
	r, ok := f.responses[rawIDToken]
	if !ok {
		return verify.GoogleClaims{}, errUnknownToken
	}
	return r.claims, r.err
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
	if acc.GetAccount().GetId() != "acc-1" || acc.GetAccount().GetPseudonym() != "shadow_fox" || acc.GetAccount().GetGoogleLinked() {
		t.Fatalf("unexpected account: %+v", acc)
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
	if err != nil || acc.GetAccount().GetPseudonym() != "night_owl" {
		t.Fatalf("UpdatePseudonym: acc=%+v err=%v", acc, err)
	}

	if _, err := s.UpdatePseudonym(context.Background(), &identityv1.UpdatePseudonymRequest{AccountId: "missing", NewPseudonym: "x"}); status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
	if _, err := s.UpdatePseudonym(context.Background(), &identityv1.UpdatePseudonymRequest{AccountId: "acc-1", NewPseudonym: ""}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for empty pseudonym, got %v", err)
	}
}

func TestSignInWithGoogle(t *testing.T) {
	verifier := &fakeVerifier{responses: map[string]fakeVerifierResponse{
		"good-token":    {claims: verify.GoogleClaims{Sub: "sub-1", Email: "aro@kiit.ac.in", EmailVerified: true, HD: "kiit.ac.in"}},
		"same-sub-tok":  {claims: verify.GoogleClaims{Sub: "sub-1", Email: "aro@kiit.ac.in", EmailVerified: true, HD: "kiit.ac.in"}},
		"other-sub-tok": {claims: verify.GoogleClaims{Sub: "sub-2", Email: "other@kiit.ac.in", EmailVerified: true, HD: "kiit.ac.in"}},
		"unverified":    {claims: verify.GoogleClaims{Sub: "sub-3", Email: "x@kiit.ac.in", EmailVerified: false, HD: "kiit.ac.in"}},
		"wrong-domain":  {claims: verify.GoogleClaims{Sub: "sub-4", Email: "x@gmail.com", EmailVerified: true}},
	}}

	t.Run("case C: no guest account creates a new verified account with a generated pseudonym", func(t *testing.T) {
		s := New(newFakeStore(), verifier, idGen("acc-1"), "kiit.ac.in")
		resp, err := s.SignInWithGoogle(context.Background(), &identityv1.SignInWithGoogleRequest{IdToken: "good-token"})
		if err != nil {
			t.Fatalf("SignInWithGoogle: %v", err)
		}
		acc := resp.GetAccount()
		if acc.GetId() != "acc-1" || !acc.GetGoogleLinked() || acc.GetPseudonym() == "" {
			t.Fatalf("unexpected account: %+v", acc)
		}
		if !resp.GetNeedsGender() {
			t.Fatalf("expected needs_gender for a brand new account")
		}
	})

	t.Run("case B: a guest account is upgraded in place", func(t *testing.T) {
		st := newFakeStore()
		s := New(st, verifier, idGen("guest-1"), "kiit.ac.in")
		if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"}); err != nil {
			t.Fatalf("create guest: %v", err)
		}

		resp, err := s.SignInWithGoogle(context.Background(), &identityv1.SignInWithGoogleRequest{
			IdToken: "good-token", GuestAccountId: strPtr("guest-1"),
		})
		if err != nil {
			t.Fatalf("SignInWithGoogle: %v", err)
		}
		acc := resp.GetAccount()
		if acc.GetId() != "guest-1" || acc.GetPseudonym() != "shadow_fox" || !acc.GetGoogleLinked() {
			t.Fatalf("expected the guest account upgraded in place, got %+v", acc)
		}
	})

	t.Run("case A: an already-linked account signs in again and is returned unchanged", func(t *testing.T) {
		st := newFakeStore()
		s := New(st, verifier, idGen("acc-1"), "kiit.ac.in")
		first, err := s.SignInWithGoogle(context.Background(), &identityv1.SignInWithGoogleRequest{IdToken: "good-token"})
		if err != nil {
			t.Fatalf("first sign-in: %v", err)
		}

		second, err := s.SignInWithGoogle(context.Background(), &identityv1.SignInWithGoogleRequest{IdToken: "same-sub-tok"})
		if err != nil {
			t.Fatalf("second sign-in: %v", err)
		}
		if second.GetAccount().GetId() != first.GetAccount().GetId() {
			t.Fatalf("expected the same account both times, got %s then %s", first.GetAccount().GetId(), second.GetAccount().GetId())
		}
	})

	t.Run("a different google identity on an already-linked guest id is rejected, not merged", func(t *testing.T) {
		st := newFakeStore()
		s := New(st, verifier, idGen("acc-1", "guest-2"), "kiit.ac.in")
		if _, err := s.SignInWithGoogle(context.Background(), &identityv1.SignInWithGoogleRequest{IdToken: "good-token"}); err != nil {
			t.Fatalf("first sign-in: %v", err)
		}
		if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "other_guest"}); err != nil {
			t.Fatalf("create second guest: %v", err)
		}

		// A second, different Google identity trying to claim the first
		// one's email is a collision, not a merge - acc-1 already owns it.
		verifierWithCollision := &fakeVerifier{responses: map[string]fakeVerifierResponse{
			"colliding-email": {claims: verify.GoogleClaims{Sub: "sub-colliding", Email: "aro@kiit.ac.in", EmailVerified: true, HD: "kiit.ac.in"}},
		}}
		s2 := New(st, verifierWithCollision, idGen(), "kiit.ac.in")
		_, err := s2.SignInWithGoogle(context.Background(), &identityv1.SignInWithGoogleRequest{
			IdToken: "colliding-email", GuestAccountId: strPtr("guest-2"),
		})
		if status.Code(err) != codes.AlreadyExists {
			t.Fatalf("expected AlreadyExists, got %v", err)
		}
	})

	t.Run("unverified email rejected", func(t *testing.T) {
		s := New(newFakeStore(), verifier, idGen(), "kiit.ac.in")
		_, err := s.SignInWithGoogle(context.Background(), &identityv1.SignInWithGoogleRequest{IdToken: "unverified"})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got %v", err)
		}
	})

	t.Run("wrong hd rejected", func(t *testing.T) {
		s := New(newFakeStore(), verifier, idGen(), "kiit.ac.in")
		_, err := s.SignInWithGoogle(context.Background(), &identityv1.SignInWithGoogleRequest{IdToken: "wrong-domain"})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("expected PermissionDenied, got %v", err)
		}
	})

	t.Run("a guest id that doesn't exist falls through to creating a new account", func(t *testing.T) {
		s := New(newFakeStore(), verifier, idGen("acc-1"), "kiit.ac.in")
		resp, err := s.SignInWithGoogle(context.Background(), &identityv1.SignInWithGoogleRequest{
			IdToken: "other-sub-tok", GuestAccountId: strPtr("no-such-guest"),
		})
		if err != nil {
			t.Fatalf("SignInWithGoogle: %v", err)
		}
		if resp.GetAccount().GetId() != "acc-1" {
			t.Fatalf("expected a newly created account, got %+v", resp.GetAccount())
		}
	})
}

func TestSetGender(t *testing.T) {
	st := newFakeStore()
	s := New(st, &fakeVerifier{}, idGen("acc-1"), "kiit.ac.in")
	if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	resp, err := s.SetGender(context.Background(), &identityv1.SetGenderRequest{AccountId: "acc-1", Gender: identityv1.Gender_GENDER_FEMALE})
	if err != nil {
		t.Fatalf("SetGender: %v", err)
	}
	if !resp.GetAccount().GetGenderChosen() || resp.GetAccount().GetGender() != identityv1.Gender_GENDER_FEMALE {
		t.Fatalf("unexpected account: %+v", resp.GetAccount())
	}

	_, err = s.SetGender(context.Background(), &identityv1.SetGenderRequest{AccountId: "acc-1", Gender: identityv1.Gender_GENDER_MALE})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition on a second call, got %v", err)
	}

	if _, err := s.SetGender(context.Background(), &identityv1.SetGenderRequest{AccountId: "acc-1", Gender: identityv1.Gender_GENDER_UNSPECIFIED}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for unspecified gender, got %v", err)
	}

	if _, err := s.SetGender(context.Background(), &identityv1.SetGenderRequest{AccountId: "missing", Gender: identityv1.Gender_GENDER_MALE}); status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestAdminSetGender(t *testing.T) {
	st := newFakeStore()
	s := New(st, &fakeVerifier{}, idGen("acc-1"), "kiit.ac.in")
	if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.SetGender(context.Background(), &identityv1.SetGenderRequest{AccountId: "acc-1", Gender: identityv1.Gender_GENDER_FEMALE}); err != nil {
		t.Fatalf("initial SetGender: %v", err)
	}

	// Unlike SetGender, a second (admin) change is allowed.
	resp, err := s.AdminSetGender(context.Background(), &identityv1.AdminSetGenderRequest{
		AccountId: "acc-1", Gender: identityv1.Gender_GENDER_PREFER_NOT_TO_SAY, Reason: "user requested correction",
	})
	if err != nil {
		t.Fatalf("AdminSetGender: %v", err)
	}
	if resp.GetAccount().GetGender() != identityv1.Gender_GENDER_PREFER_NOT_TO_SAY {
		t.Fatalf("unexpected account: %+v", resp.GetAccount())
	}

	if _, err := s.AdminSetGender(context.Background(), &identityv1.AdminSetGenderRequest{AccountId: "missing", Gender: identityv1.Gender_GENDER_MALE}); status.Code(err) != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestTouchAccount(t *testing.T) {
	st := newFakeStore()
	s := New(st, &fakeVerifier{}, idGen("acc-1"), "kiit.ac.in")
	if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "shadow_fox"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.TouchAccount(context.Background(), &identityv1.TouchAccountRequest{AccountId: "acc-1"}); err != nil {
		t.Fatalf("TouchAccount: %v", err)
	}
	// A missing account is not an error (DEC-0026, best-effort liveness signal).
	if _, err := s.TouchAccount(context.Background(), &identityv1.TouchAccountRequest{AccountId: "missing"}); err != nil {
		t.Fatalf("TouchAccount for a missing account should not error, got %v", err)
	}
}

func strPtr(s string) *string { return &s }

// A guest who does not choose a name is given one (INC-0025). The web app used
// to pick from only 256 names and failed with "pseudonym already taken" as
// accounts accumulated; the server now chooses and retries.
func TestCreateAccountWithoutAPseudonymGeneratesOne(t *testing.T) {
	s := New(newFakeStore(), &fakeVerifier{}, idGen("acc-1"), "kiit.ac.in")
	for _, blank := range []string{"", "   "} {
		resp, err := New(newFakeStore(), &fakeVerifier{}, idGen("acc-1"), "kiit.ac.in").CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: blank})
		if err != nil {
			t.Fatalf("CreateAccount(%q): %v", blank, err)
		}
		if got := resp.GetAccount().GetPseudonym(); len(strings.Fields(got)) < 2 {
			t.Fatalf("expected a generated two-word name for %q, got %q", blank, got)
		}
	}
	_ = s
}

func TestCreateAccountRetriesWhenAGeneratedNameIsTaken(t *testing.T) {
	st := newFakeStore()
	s := New(st, &fakeVerifier{}, idGen("acc-0", "acc-1", "acc-2", "acc-3"), "kiit.ac.in")
	for _, taken := range []string{"Taken One", "Taken Two"} {
		if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: taken}); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	attempts := 0
	defer func(prev func(int) string) { nextPseudonym = prev }(nextPseudonym)
	nextPseudonym = func(int) string {
		attempts++
		if attempts <= 2 {
			return map[int]string{1: "Taken One", 2: "Taken Two"}[attempts]
		}
		return "Free Name"
	}

	resp, err := New(st, &fakeVerifier{}, idGen("acc-a", "acc-b", "acc-c"), "kiit.ac.in").CreateAccount(context.Background(), &identityv1.CreateAccountRequest{})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if resp.GetAccount().GetPseudonym() != "Free Name" || attempts != 3 {
		t.Fatalf("expected the third generated name to be used, got %q after %d attempts", resp.GetAccount().GetPseudonym(), attempts)
	}
}

func TestCreateAccountGivesUpPolitelyWhenNoFreeNameIsFound(t *testing.T) {
	st := newFakeStore()
	ids := make([]string, 0, maxPseudonymAttempts+1)
	for i := range maxPseudonymAttempts + 1 {
		ids = append(ids, fmt.Sprintf("acc-%d", i))
	}
	s := New(st, &fakeVerifier{}, idGen(ids...), "kiit.ac.in")
	if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{Pseudonym: "Always Taken"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer func(prev func(int) string) { nextPseudonym = prev }(nextPseudonym)
	nextPseudonym = func(int) string { return "Always Taken" }

	if _, err := s.CreateAccount(context.Background(), &identityv1.CreateAccountRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("expected Unavailable (try again) when no free name is found, got %v", err)
	}
}

func TestGeneratedNamesGetANumberAfterAFewClashes(t *testing.T) {
	plain := regexp.MustCompile(`^[A-Z][a-z]+ [A-Z][a-z]+$`)
	numbered := regexp.MustCompile(`^[A-Z][a-z]+ [A-Z][a-z]+ \d{2}$`)
	for attempt := range pseudonymPlainAttempts {
		if got := generatePseudonymAttempt(attempt); !plain.MatchString(got) {
			t.Fatalf("attempt %d should be two plain words, got %q", attempt, got)
		}
	}
	for attempt := pseudonymPlainAttempts; attempt < maxPseudonymAttempts; attempt++ {
		if got := generatePseudonymAttempt(attempt); !numbered.MatchString(got) {
			t.Fatalf("attempt %d should end in a two-digit number, got %q", attempt, got)
		}
	}
}
