// Package server implements the ModerationService gRPC contract
// (docs/ARCHITECTURE.md §3.5), translating between the wire types
// (moderationv1) and the store/sets/events/identity boundaries.
package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/moderation/internal/events"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/moderation/internal/sets"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/moderation/internal/store"
)

// IDGenerator generates a new report, request or event ID - the same
// pattern every other service's server package uses, so tests can
// supply deterministic IDs.
type IDGenerator func() string

// IdentityClient is the subset of identityv1.IdentityServiceClient this
// package calls, narrowed so fakes_test.go only has to implement what's
// used.
type IdentityClient interface {
	AdminSetGender(ctx context.Context, req *identityv1.AdminSetGenderRequest, opts ...grpc.CallOption) (*identityv1.AdminSetGenderResponse, error)
}

type Server struct {
	moderationv1.UnimplementedModerationServiceServer

	store    store.Store
	sets     sets.Sets
	producer events.Producer
	identity IdentityClient
	genID    IDGenerator
}

func New(s store.Store, st sets.Sets, producer events.Producer, identity IdentityClient, genID IDGenerator) *Server {
	return &Server{store: s, sets: st, producer: producer, identity: identity, genID: genID}
}

func (s *Server) FileReport(ctx context.Context, req *moderationv1.FileReportRequest) (*moderationv1.FileReportResponse, error) {
	reporterID := strings.TrimSpace(req.GetReporterAccountId())
	reportedID := strings.TrimSpace(req.GetReportedAccountId())
	reason := strings.TrimSpace(req.GetReason())
	if reporterID == "" || reportedID == "" || reason == "" {
		return nil, status.Error(codes.InvalidArgument, "reporter_account_id, reported_account_id and reason are required")
	}

	id := s.genID()
	if _, err := s.store.FileReport(ctx, id, reporterID, reportedID, req.GetRoomId(), reason); err != nil {
		return nil, status.Errorf(codes.Internal, "file report: %v", err)
	}

	event := &eventsv1.ModerationEvent{
		EventId: s.genID(), OccurredAtUnix: time.Now().Unix(),
		Payload: &eventsv1.ModerationEvent_ReportFiled{ReportFiled: &eventsv1.ReportFiled{
			ReportId: id, ReporterAccountId: reporterID, ReportedAccountId: reportedID,
		}},
	}
	if err := s.producer.Publish(ctx, reporterID, event); err != nil {
		return nil, status.Errorf(codes.Internal, "publish report filed: %v", err)
	}
	return &moderationv1.FileReportResponse{ReportId: id}, nil
}

func (s *Server) BlockUser(ctx context.Context, req *moderationv1.BlockUserRequest) (*moderationv1.BlockUserResponse, error) {
	accountID := strings.TrimSpace(req.GetAccountId())
	blockedID := strings.TrimSpace(req.GetBlockedAccountId())
	if accountID == "" || blockedID == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id and blocked_account_id are required")
	}
	if accountID == blockedID {
		return nil, status.Error(codes.InvalidArgument, "an account cannot block itself")
	}

	if err := s.store.BlockUser(ctx, accountID, blockedID); err != nil {
		return nil, status.Errorf(codes.Internal, "block user: %v", err)
	}
	if err := s.sets.AddMutualAvoid(ctx, accountID, blockedID); err != nil {
		return nil, status.Errorf(codes.Internal, "add mutual avoid: %v", err)
	}

	event := &eventsv1.ModerationEvent{
		EventId: s.genID(), OccurredAtUnix: time.Now().Unix(),
		Payload: &eventsv1.ModerationEvent_UserBlocked{UserBlocked: &eventsv1.UserBlocked{
			AccountId: accountID, BlockedAccountId: blockedID,
		}},
	}
	if err := s.producer.Publish(ctx, accountID, event); err != nil {
		return nil, status.Errorf(codes.Internal, "publish user blocked: %v", err)
	}
	return &moderationv1.BlockUserResponse{Blocked: true}, nil
}

func (s *Server) ListBlocks(ctx context.Context, req *moderationv1.ListBlocksRequest) (*moderationv1.ListBlocksResponse, error) {
	accountID := strings.TrimSpace(req.GetAccountId())
	if accountID == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}
	ids, err := s.store.ListBlocks(ctx, accountID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list blocks: %v", err)
	}
	return &moderationv1.ListBlocksResponse{BlockedAccountIds: ids}, nil
}

func (s *Server) RequestGenderChange(ctx context.Context, req *moderationv1.RequestGenderChangeRequest) (*moderationv1.RequestGenderChangeResponse, error) {
	accountID := strings.TrimSpace(req.GetAccountId())
	if accountID == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}
	if req.GetRequestedGender() == identityv1.Gender_GENDER_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "requested_gender must be specified")
	}

	id := s.genID()
	if _, err := s.store.RequestGenderChange(ctx, id, accountID, req.GetRequestedGender(), req.GetNote()); err != nil {
		return nil, status.Errorf(codes.Internal, "request gender change: %v", err)
	}
	return &moderationv1.RequestGenderChangeResponse{RequestId: id}, nil
}

func (s *Server) ListReports(ctx context.Context, req *moderationv1.ListReportsRequest) (*moderationv1.ListReportsResponse, error) {
	reports, err := s.store.ListReports(ctx, req.GetOnlyUnresolved())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list reports: %v", err)
	}
	out := make([]*moderationv1.Report, 0, len(reports))
	for _, r := range reports {
		out = append(out, &moderationv1.Report{
			Id: r.ID, ReporterAccountId: r.ReporterAccountID, ReportedAccountId: r.ReportedAccountID,
			RoomId: r.RoomID, Reason: r.Reason, Resolved: r.Resolved, FiledAtUnix: r.FiledAt.Unix(),
		})
	}
	return &moderationv1.ListReportsResponse{Reports: out}, nil
}

func (s *Server) ResolveReport(ctx context.Context, req *moderationv1.ResolveReportRequest) (*moderationv1.ResolveReportResponse, error) {
	reportID := strings.TrimSpace(req.GetReportId())
	resolvedBy := strings.TrimSpace(req.GetResolvedBy())
	if reportID == "" || resolvedBy == "" {
		return nil, status.Error(codes.InvalidArgument, "report_id and resolved_by are required")
	}

	if err := s.store.ResolveReport(ctx, reportID, resolvedBy, req.GetOutcome()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "report not found")
		}
		return nil, status.Errorf(codes.Internal, "resolve report: %v", err)
	}
	if err := s.logAdminAction(ctx, resolvedBy, "resolve_report", reportID, req.GetOutcome()); err != nil {
		return nil, err
	}
	return &moderationv1.ResolveReportResponse{}, nil
}

func (s *Server) BanAccount(ctx context.Context, req *moderationv1.BanAccountRequest) (*moderationv1.BanAccountResponse, error) {
	accountID := strings.TrimSpace(req.GetAccountId())
	bannedBy := strings.TrimSpace(req.GetBannedBy())
	reason := strings.TrimSpace(req.GetReason())
	if accountID == "" || bannedBy == "" || reason == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id, banned_by and reason are required")
	}

	if err := s.store.BanAccount(ctx, accountID, reason, bannedBy); err != nil {
		return nil, status.Errorf(codes.Internal, "ban account: %v", err)
	}
	if err := s.sets.Ban(ctx, accountID); err != nil {
		return nil, status.Errorf(codes.Internal, "add to banned set: %v", err)
	}
	if err := s.logAdminAction(ctx, bannedBy, "ban_account", accountID, reason); err != nil {
		return nil, err
	}

	event := &eventsv1.ModerationEvent{
		EventId: s.genID(), OccurredAtUnix: time.Now().Unix(),
		Payload: &eventsv1.ModerationEvent_AccountBanned{AccountBanned: &eventsv1.AccountBanned{
			AccountId: accountID, Reason: reason,
		}},
	}
	if err := s.producer.Publish(ctx, accountID, event); err != nil {
		return nil, status.Errorf(codes.Internal, "publish account banned: %v", err)
	}
	return &moderationv1.BanAccountResponse{}, nil
}

func (s *Server) UnbanAccount(ctx context.Context, req *moderationv1.UnbanAccountRequest) (*moderationv1.UnbanAccountResponse, error) {
	accountID := strings.TrimSpace(req.GetAccountId())
	unbannedBy := strings.TrimSpace(req.GetUnbannedBy())
	if accountID == "" || unbannedBy == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id and unbanned_by are required")
	}

	if err := s.store.UnbanAccount(ctx, accountID, unbannedBy); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "account has no ban record")
		}
		return nil, status.Errorf(codes.Internal, "unban account: %v", err)
	}
	if err := s.sets.Unban(ctx, accountID); err != nil {
		return nil, status.Errorf(codes.Internal, "remove from banned set: %v", err)
	}
	if err := s.logAdminAction(ctx, unbannedBy, "unban_account", accountID, ""); err != nil {
		return nil, err
	}

	event := &eventsv1.ModerationEvent{
		EventId: s.genID(), OccurredAtUnix: time.Now().Unix(),
		Payload: &eventsv1.ModerationEvent_AccountUnbanned{AccountUnbanned: &eventsv1.AccountUnbanned{AccountId: accountID}},
	}
	if err := s.producer.Publish(ctx, accountID, event); err != nil {
		return nil, status.Errorf(codes.Internal, "publish account unbanned: %v", err)
	}
	return &moderationv1.UnbanAccountResponse{}, nil
}

func (s *Server) ListGenderChangeRequests(ctx context.Context, req *moderationv1.ListGenderChangeRequestsRequest) (*moderationv1.ListGenderChangeRequestsResponse, error) {
	requests, err := s.store.ListGenderChangeRequests(ctx, req.GetOnlyUnresolved())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list gender change requests: %v", err)
	}
	out := make([]*moderationv1.GenderChangeRequest, 0, len(requests))
	for _, g := range requests {
		out = append(out, &moderationv1.GenderChangeRequest{
			Id: g.ID, AccountId: g.AccountID, RequestedGender: g.RequestedGender, Note: g.Note,
			Resolved: g.Resolved, Approved: g.Approved, RequestedAtUnix: g.RequestedAt.Unix(),
		})
	}
	return &moderationv1.ListGenderChangeRequestsResponse{Requests: out}, nil
}

// ResolveGenderChangeRequest records the decision in moderation's own
// table, and - only when approving - also calls Identity.AdminSetGender
// so the account's gender actually changes. Moderation is the only
// service with an identity client for this specific purpose, consistent
// with Matching being the only service with a chat client for creating
// direct rooms.
func (s *Server) ResolveGenderChangeRequest(ctx context.Context, req *moderationv1.ResolveGenderChangeRequestRequest) (*moderationv1.ResolveGenderChangeRequestResponse, error) {
	requestID := strings.TrimSpace(req.GetRequestId())
	resolvedBy := strings.TrimSpace(req.GetResolvedBy())
	if requestID == "" || resolvedBy == "" {
		return nil, status.Error(codes.InvalidArgument, "request_id and resolved_by are required")
	}

	requests, err := s.store.ListGenderChangeRequests(ctx, false)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list gender change requests: %v", err)
	}
	var target *store.GenderChangeRequest
	for i := range requests {
		if requests[i].ID == requestID {
			target = &requests[i]
			break
		}
	}
	if target == nil {
		return nil, status.Error(codes.NotFound, "gender change request not found")
	}

	if err := s.store.ResolveGenderChangeRequest(ctx, requestID, req.GetApprove(), resolvedBy); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "gender change request not found")
		}
		return nil, status.Errorf(codes.Internal, "resolve gender change request: %v", err)
	}

	action := "reject_gender_change"
	if req.GetApprove() {
		action = "approve_gender_change"
		if _, err := s.identity.AdminSetGender(ctx, &identityv1.AdminSetGenderRequest{
			AccountId: target.AccountID, Gender: target.RequestedGender, Reason: "approved gender change request " + requestID,
		}); err != nil {
			return nil, status.Errorf(codes.Internal, "admin set gender: %v", err)
		}
	}
	if err := s.logAdminAction(ctx, resolvedBy, action, target.AccountID, requestID); err != nil {
		return nil, err
	}
	return &moderationv1.ResolveGenderChangeRequestResponse{}, nil
}

func (s *Server) logAdminAction(ctx context.Context, actorAccountID, action, targetAccountID, detail string) error {
	if err := s.store.LogAdminAction(ctx, s.genID(), store.AdminAction{
		ActorAccountID: actorAccountID, Action: action, TargetAccountID: targetAccountID, Detail: detail,
	}); err != nil {
		return status.Errorf(codes.Internal, "log admin action: %v", err)
	}
	return nil
}
