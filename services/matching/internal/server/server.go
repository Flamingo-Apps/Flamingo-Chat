// Package server implements the MatchingService gRPC contract,
// orchestrating the queue (who's waiting), Chat (creating and ending
// direct rooms) and events (telling the delivery worker about a match)
// per docs/ARCHITECTURE.md §3.2.
package server

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	matchingv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/matching/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/events"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/pairing"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/queue"
)

// IDGenerator generates a new event ID - the same pattern identity's and
// chat's server packages use, so tests can supply deterministic IDs.
type IDGenerator func() string

// ChatClient is the subset of chatv1.ChatServiceClient this package
// calls, narrowed so fakes_test.go only has to implement what's used.
type ChatClient interface {
	CreateDirectRoom(ctx context.Context, req *chatv1.CreateDirectRoomRequest, opts ...grpc.CallOption) (*chatv1.CreateDirectRoomResponse, error)
	EndDirectRoom(ctx context.Context, req *chatv1.EndDirectRoomRequest, opts ...grpc.CallOption) (*chatv1.EndDirectRoomResponse, error)
}

type Server struct {
	matchingv1.UnimplementedMatchingServiceServer

	queue    queue.Queue
	pairing  pairing.Cache
	chat     ChatClient
	producer events.Producer
	genID    IDGenerator
}

func New(q queue.Queue, pairingCache pairing.Cache, chat ChatClient, producer events.Producer, genID IDGenerator) *Server {
	return &Server{queue: q, pairing: pairingCache, chat: chat, producer: producer, genID: genID}
}

func (s *Server) RequestMatch(ctx context.Context, req *matchingv1.RequestMatchRequest) (*matchingv1.RequestMatchResponse, error) {
	accountID := strings.TrimSpace(req.GetAccountId())
	if accountID == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}
	pseudonym := strings.TrimSpace(req.GetPseudonym())
	if pseudonym == "" {
		return nil, status.Error(codes.InvalidArgument, "pseudonym is required")
	}
	preference, err := preferenceToString(req.GetPreference())
	if err != nil {
		return nil, err
	}

	seeker := queue.Candidate{
		AccountID:  accountID,
		Preference: preference,
		Gender:     genderToString(req.GetGender()),
		Verified:   req.GetVerified(),
		Pseudonym:  pseudonym,
	}
	if err := checkGenderGate(seeker); err != nil {
		return nil, err
	}

	if err := s.tryMatchAndRoom(ctx, seeker); err != nil {
		return nil, err
	}
	return &matchingv1.RequestMatchResponse{Queued: true}, nil
}

func (s *Server) CancelMatch(ctx context.Context, req *matchingv1.CancelMatchRequest) (*matchingv1.CancelMatchResponse, error) {
	accountID := strings.TrimSpace(req.GetAccountId())
	if accountID == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}
	cancelled, err := s.queue.Cancel(ctx, accountID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "cancel: %v", err)
	}
	return &matchingv1.CancelMatchResponse{Cancelled: cancelled}, nil
}

// Next ends the caller's current direct room through Chat - the same
// EndDirectRoom call the gateway's own match_end handler already makes
// directly (DEC-0074) - then tries to re-enqueue the caller with
// whatever attributes they originally matched with, cached by
// RequestMatch when the room was created. docs/ARCHITECTURE.md §5.2
// gives match_next's client frame no fields of its own, so without that
// cache there would be nothing to re-enqueue with (DEC-0078).
func (s *Server) Next(ctx context.Context, req *matchingv1.NextRequest) (*matchingv1.NextResponse, error) {
	accountID := strings.TrimSpace(req.GetAccountId())
	roomID := strings.TrimSpace(req.GetCurrentRoomId())
	if accountID == "" || roomID == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id and current_room_id are required")
	}

	if _, err := s.chat.EndDirectRoom(ctx, &chatv1.EndDirectRoomRequest{
		RoomId: roomID, AccountId: accountID, Reason: "next",
	}); err != nil {
		return nil, err
	}

	a, b, found, err := s.pairing.Get(ctx, roomID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "pairing lookup: %v", err)
	}
	if !found {
		// The room ended either way (above); re-queuing automatically is
		// just not possible without the cached attributes. The client is
		// left to send a fresh match_request with an explicit preference.
		return &matchingv1.NextResponse{Queued: false}, nil
	}
	mine, partner := a, b
	if a.AccountID != accountID {
		mine, partner = b, a
	}
	if mine.AccountID != accountID {
		// Neither cached side is this account - a corrupted or mismatched
		// cache entry. The room still ended correctly above; treat this
		// the same as a cache miss rather than erroring after the fact.
		return &matchingv1.NextResponse{Queued: false}, nil
	}
	if err := s.pairing.Delete(ctx, roomID); err != nil {
		return nil, status.Errorf(codes.Internal, "pairing delete: %v", err)
	}

	seeker := queue.Candidate{
		AccountID: mine.AccountID, Preference: mine.Preference, Gender: mine.Gender,
		Verified: mine.Verified, Pseudonym: mine.Pseudonym, PreviousPartner: partner.AccountID,
	}
	if err := s.tryMatchAndRoom(ctx, seeker); err != nil {
		return nil, err
	}
	return &matchingv1.NextResponse{Queued: true}, nil
}

// tryMatchAndRoom runs docs/ARCHITECTURE.md §3.2's algorithm for seeker:
// look for a compatible waiting candidate, and if one exists, create the
// direct room, publish MatchFound, and cache both sides' attributes for
// a future Next - all of which RequestMatch and Next both need, the only
// difference between them being how seeker was built.
func (s *Server) tryMatchAndRoom(ctx context.Context, seeker queue.Candidate) error {
	result, err := s.queue.TryMatch(ctx, seeker)
	if err != nil {
		return status.Errorf(codes.Internal, "match: %v", err)
	}
	if !result.Matched {
		return nil
	}
	partner := result.Partner

	roomResp, err := s.chat.CreateDirectRoom(ctx, &chatv1.CreateDirectRoomRequest{
		MemberAccountIds: []string{seeker.AccountID, partner.AccountID},
	})
	if err != nil {
		// Both accounts were already removed from the queue by the atomic
		// match above; losing them silently on a failure to actually
		// create the room would strand them with no visible queue entry
		// and no room. Best-effort re-enqueue both rather than leaving
		// that gap - a saga/outbox would be the thorough fix, not
		// justified at this project's scale (DEC-0047's same reasoning
		// for failure handling elsewhere).
		s.requeueBestEffort(ctx, seeker)
		s.requeueBestEffort(ctx, partner)
		return status.Errorf(codes.Internal, "create direct room: %v", err)
	}
	room := roomResp.GetRoom()

	if err := s.pairing.Save(ctx, room.GetId(), pairing.FromCandidate(seeker), pairing.FromCandidate(partner)); err != nil {
		return status.Errorf(codes.Internal, "pairing save: %v", err)
	}

	event := &eventsv1.MatchingEvent{
		EventId:        s.genID(),
		OccurredAtUnix: time.Now().Unix(),
		Payload: &eventsv1.MatchingEvent_MatchFound{MatchFound: &eventsv1.MatchFound{
			RoomId:     room.GetId(),
			AccountIdA: seeker.AccountID,
			AccountIdB: partner.AccountID,
			PseudonymA: seeker.Pseudonym,
			PseudonymB: partner.Pseudonym,
		}},
	}
	if err := s.producer.Publish(ctx, room.GetId(), event); err != nil {
		return status.Errorf(codes.Internal, "publish match found: %v", err)
	}
	return nil
}

func (s *Server) requeueBestEffort(ctx context.Context, c queue.Candidate) {
	_, _ = s.queue.TryMatch(ctx, c)
}

// checkGenderGate enforces docs/ARCHITECTURE.md §3.2's "preference =
// male | female requires the seeker to be verified with a chosen gender
// of male or female" - gender-filtered matching is a verified-account
// feature (the PRD's access tiers), not something an unverified guest
// can request regardless of who might be waiting.
func checkGenderGate(c queue.Candidate) error {
	if c.Preference == queue.PreferenceAny {
		return nil
	}
	if c.Verified && (c.Gender == queue.GenderMale || c.Gender == queue.GenderFemale) {
		return nil
	}
	return status.Error(codes.PermissionDenied, "gender-filtered matching requires a verified account with a chosen gender")
}

func preferenceToString(p matchingv1.Preference) (string, error) {
	switch p {
	case matchingv1.Preference_PREFERENCE_ANY:
		return queue.PreferenceAny, nil
	case matchingv1.Preference_PREFERENCE_MALE:
		return queue.PreferenceMale, nil
	case matchingv1.Preference_PREFERENCE_FEMALE:
		return queue.PreferenceFemale, nil
	default:
		return "", status.Errorf(codes.InvalidArgument, "preference must be specified")
	}
}

func genderToString(g identityv1.Gender) string {
	switch g {
	case identityv1.Gender_GENDER_MALE:
		return queue.GenderMale
	case identityv1.Gender_GENDER_FEMALE:
		return queue.GenderFemale
	case identityv1.Gender_GENDER_PREFER_NOT_TO_SAY:
		return "prefer_not_to_say"
	default:
		return "unspecified"
	}
}
