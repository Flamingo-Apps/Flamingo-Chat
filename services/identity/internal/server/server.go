// Package server implements the IdentityService gRPC contract, translating
// between the wire types (identityv1) and the store/verify boundaries.
package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/internal/store"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/identity/internal/verify"
)

// IDGenerator generates a new account ID. A function type rather than a
// concrete uuid.NewString reference so tests can supply deterministic IDs.
type IDGenerator func() string

// maxPseudonymAttempts bounds how many generated pseudonyms SignInWithGoogle
// tries before giving up - see createVerifiedWithGeneratedPseudonym.
const maxPseudonymAttempts = 10

type Server struct {
	identityv1.UnimplementedIdentityServiceServer

	store         store.AccountStore
	verifier      verify.GoogleVerifier
	genID         IDGenerator
	collegeDomain string
}

func New(accountStore store.AccountStore, verifier verify.GoogleVerifier, genID IDGenerator, collegeDomain string) *Server {
	return &Server{
		store:         accountStore,
		verifier:      verifier,
		genID:         genID,
		collegeDomain: strings.ToLower(collegeDomain),
	}
}

func (s *Server) CreateAccount(ctx context.Context, req *identityv1.CreateAccountRequest) (*identityv1.CreateAccountResponse, error) {
	if strings.TrimSpace(req.GetPseudonym()) == "" {
		return nil, status.Error(codes.InvalidArgument, "pseudonym must not be empty")
	}

	acc, err := s.store.Create(ctx, s.genID(), req.GetPseudonym())
	if errors.Is(err, store.ErrAlreadyExists) {
		return nil, status.Error(codes.AlreadyExists, "pseudonym already taken")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create account: %v", err)
	}
	return &identityv1.CreateAccountResponse{Account: toProto(acc)}, nil
}

func (s *Server) GetAccount(ctx context.Context, req *identityv1.GetAccountRequest) (*identityv1.GetAccountResponse, error) {
	acc, err := s.store.Get(ctx, req.GetAccountId())
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "account not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get account: %v", err)
	}
	return &identityv1.GetAccountResponse{Account: toProto(acc)}, nil
}

func (s *Server) UpdatePseudonym(ctx context.Context, req *identityv1.UpdatePseudonymRequest) (*identityv1.UpdatePseudonymResponse, error) {
	if strings.TrimSpace(req.GetNewPseudonym()) == "" {
		return nil, status.Error(codes.InvalidArgument, "new pseudonym must not be empty")
	}

	acc, err := s.store.UpdatePseudonym(ctx, req.GetAccountId(), req.GetNewPseudonym())
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "account not found")
	}
	if errors.Is(err, store.ErrAlreadyExists) {
		return nil, status.Error(codes.AlreadyExists, "pseudonym already taken")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update pseudonym: %v", err)
	}
	return &identityv1.UpdatePseudonymResponse{Account: toProto(acc)}, nil
}

// SignInWithGoogle covers all three cases from docs/ARCHITECTURE.md §4.2:
// an existing verified account, upgrading a guest account in place, or
// creating a new verified account. hd and email_verified are checked
// here rather than trusting the email's suffix (DEC-0027, INC-0006).
func (s *Server) SignInWithGoogle(ctx context.Context, req *identityv1.SignInWithGoogleRequest) (*identityv1.SignInWithGoogleResponse, error) {
	claims, err := s.verifier.Verify(ctx, req.GetIdToken())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "id token did not verify")
	}
	if !claims.EmailVerified {
		return nil, status.Error(codes.InvalidArgument, "email is not verified")
	}
	if !strings.EqualFold(claims.HD, s.collegeDomain) {
		return nil, status.Errorf(codes.PermissionDenied, "account is not managed by %s", s.collegeDomain)
	}

	// Case A: this Google identity is already linked to an account.
	if acc, err := s.store.GetByGoogleSub(ctx, claims.Sub); err == nil {
		return signInResponse(acc), nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, status.Errorf(codes.Internal, "sign in with google: %v", err)
	}

	// Case B: upgrade the caller's guest account in place, if they have one.
	if guestID := req.GetGuestAccountId(); guestID != "" {
		acc, linkErr := s.store.LinkGoogle(ctx, guestID, claims.Sub, claims.Email)
		switch {
		case linkErr == nil:
			return signInResponse(acc), nil
		case errors.Is(linkErr, store.ErrAlreadyExists):
			// A concurrent sign-in won the race and linked this Google
			// identity first. Return that account rather than creating a
			// second one for the same person.
			if acc, err := s.store.GetByGoogleSub(ctx, claims.Sub); err == nil {
				return signInResponse(acc), nil
			}
			return nil, status.Error(codes.AlreadyExists, "this college email is already linked to another account")
		case !errors.Is(linkErr, store.ErrNotFound):
			return nil, status.Errorf(codes.Internal, "sign in with google: link: %v", linkErr)
		}
		// The guest account doesn't exist (a stale or forged id) - fall
		// through to creating a fresh verified account instead of
		// failing the sign-in outright.
	}

	// Case C: no guest account to upgrade - create a new verified account
	// with a generated pseudonym.
	acc, err := s.createVerifiedWithGeneratedPseudonym(ctx, claims)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "sign in with google: create: %v", err)
	}
	return signInResponse(acc), nil
}

func (s *Server) createVerifiedWithGeneratedPseudonym(ctx context.Context, claims verify.GoogleClaims) (store.Account, error) {
	for range maxPseudonymAttempts {
		acc, err := s.store.CreateVerified(ctx, s.genID(), generatePseudonym(), claims.Sub, claims.Email)
		if err == nil {
			return acc, nil
		}
		if !errors.Is(err, store.ErrAlreadyExists) {
			return store.Account{}, err
		}
		// A collision on sub or email here would mean another request
		// linked this same Google identity between our check above and
		// this insert - vanishingly unlikely for this to be the SECOND
		// time in the same call, but if it is, retrying with a new
		// pseudonym and trying again still converges correctly, since
		// the next attempt's CreateVerified will itself collide the
		// same way until the caller gives up - what matters is this
		// loop never creates two accounts for the one Google identity.
	}
	return store.Account{}, fmt.Errorf("could not generate a unique pseudonym after %d attempts", maxPseudonymAttempts)
}

func signInResponse(acc store.Account) *identityv1.SignInWithGoogleResponse {
	return &identityv1.SignInWithGoogleResponse{
		Account:     toProto(acc),
		NeedsGender: acc.GenderSetAt == nil,
	}
}

// SetGender is deliberately callable only once per account (DEC-0028); a
// second call fails with FailedPrecondition rather than silently
// overwriting the first choice. Corrections go through AdminSetGender,
// after a reviewed request.
func (s *Server) SetGender(ctx context.Context, req *identityv1.SetGenderRequest) (*identityv1.SetGenderResponse, error) {
	if req.GetGender() == identityv1.Gender_GENDER_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "gender must be chosen")
	}
	acc, err := s.store.SetGender(ctx, req.GetAccountId(), req.GetGender())
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "account not found")
	}
	if errors.Is(err, store.ErrAlreadySet) {
		return nil, status.Error(codes.FailedPrecondition, "gender has already been chosen")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "set gender: %v", err)
	}
	return &identityv1.SetGenderResponse{Account: toProto(acc)}, nil
}

// AdminSetGender is only ever called from tools/admin (DEC-0029), after a
// request Moderation has reviewed. It does not itself record who approved
// the change or why - that log lives in Moderation's own schema
// (docs/ARCHITECTURE.md §6), so req.GetReason() is accepted here only to
// keep the contract self-describing, not persisted by Identity.
func (s *Server) AdminSetGender(ctx context.Context, req *identityv1.AdminSetGenderRequest) (*identityv1.AdminSetGenderResponse, error) {
	if req.GetGender() == identityv1.Gender_GENDER_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "gender must be chosen")
	}
	acc, err := s.store.AdminSetGender(ctx, req.GetAccountId(), req.GetGender())
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "account not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "admin set gender: %v", err)
	}
	return &identityv1.AdminSetGenderResponse{Account: toProto(acc)}, nil
}

// TouchAccount keeps last_seen_at current for guest-account cleanup
// (DEC-0026). The store throttles the actual write itself and never
// reports whether the account existed (docs/ARCHITECTURE.md §4.3), so
// this never fails for a caller passing a stale or bad account id - it's
// a best-effort liveness signal, not something worth erroring a
// connection over.
func (s *Server) TouchAccount(ctx context.Context, req *identityv1.TouchAccountRequest) (*identityv1.TouchAccountResponse, error) {
	if err := s.store.Touch(ctx, req.GetAccountId()); err != nil {
		return nil, status.Errorf(codes.Internal, "touch account: %v", err)
	}
	return &identityv1.TouchAccountResponse{}, nil
}

func toProto(acc store.Account) *identityv1.Account {
	return &identityv1.Account{
		Id:            acc.ID,
		Pseudonym:     acc.Pseudonym,
		Gender:        acc.Gender,
		CreatedAtUnix: acc.CreatedAt.Unix(),
		GoogleLinked:  acc.GoogleSub != nil,
		GenderChosen:  acc.GenderSetAt != nil,
	}
}
