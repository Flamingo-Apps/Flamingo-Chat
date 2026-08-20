// Package server implements the IdentityService gRPC contract, translating
// between the wire types (identityv1) and the store/verify boundaries.
package server

import (
	"context"
	"errors"
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

type Server struct {
	identityv1.UnimplementedIdentityServiceServer

	store         store.AccountStore
	verifier      verify.BadgeVerifier
	genID         IDGenerator
	collegeDomain string
}

func New(accountStore store.AccountStore, verifier verify.BadgeVerifier, genID IDGenerator, collegeDomain string) *Server {
	return &Server{
		store:         accountStore,
		verifier:      verifier,
		genID:         genID,
		collegeDomain: strings.ToLower(collegeDomain),
	}
}

func (s *Server) CreateAccount(ctx context.Context, req *identityv1.CreateAccountRequest) (*identityv1.Account, error) {
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
	return toProto(acc), nil
}

func (s *Server) GetAccount(ctx context.Context, req *identityv1.GetAccountRequest) (*identityv1.Account, error) {
	acc, err := s.store.Get(ctx, req.GetAccountId())
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "account not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get account: %v", err)
	}
	return toProto(acc), nil
}

func (s *Server) UpdatePseudonym(ctx context.Context, req *identityv1.UpdatePseudonymRequest) (*identityv1.Account, error) {
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
	return toProto(acc), nil
}

func (s *Server) VerifyBadge(ctx context.Context, req *identityv1.VerifyBadgeRequest) (*identityv1.Account, error) {
	if req.GetGender() == identityv1.Gender_GENDER_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "gender is required to verify a badge")
	}

	email, emailVerified, err := s.verifier.Verify(ctx, req.GetIdToken())
	if err != nil || !emailVerified {
		return nil, status.Error(codes.InvalidArgument, "id token did not verify a college email")
	}

	if !strings.HasSuffix(strings.ToLower(email), "@"+s.collegeDomain) {
		return nil, status.Errorf(codes.PermissionDenied, "email domain is not a recognized college domain")
	}

	acc, err := s.store.SetVerified(ctx, req.GetAccountId(), req.GetGender(), email)
	if errors.Is(err, store.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "account not found")
	}
	if errors.Is(err, store.ErrAlreadyExists) {
		return nil, status.Error(codes.AlreadyExists, "this college email is already verified on another account")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "verify badge: %v", err)
	}
	return toProto(acc), nil
}

func toProto(acc store.Account) *identityv1.Account {
	return &identityv1.Account{
		Id:            acc.ID,
		Pseudonym:     acc.Pseudonym,
		BadgeVerified: acc.BadgeVerified,
		Gender:        acc.Gender,
		CreatedAtUnix: acc.CreatedAt.Unix(),
	}
}
