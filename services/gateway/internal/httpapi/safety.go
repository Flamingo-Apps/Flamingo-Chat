package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
)

func (s *Server) handleFileReport(w http.ResponseWriter, r *http.Request, accountID string) {
	allowed, err := s.rateLimiter.Allow(r.Context(), "report", accountID, 10, time.Hour)
	if err != nil {
		writeError(w, status.Errorf(codes.Internal, "rate limit: %v", err))
		return
	}
	if !allowed {
		writeError(w, status.Error(codes.ResourceExhausted, "too many reports filed recently"))
		return
	}

	var req struct {
		ReportedAccountID string `json:"reported_account_id"`
		RoomID            string `json:"room_id"`
		Reason            string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, status.Error(codes.InvalidArgument, "malformed request body"))
		return
	}
	resp, err := s.moderation.FileReport(r.Context(), &moderationv1.FileReportRequest{
		ReporterAccountId: accountID, ReportedAccountId: req.ReportedAccountID, RoomId: req.RoomID, Reason: req.Reason,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		ReportID string `json:"report_id"`
	}{resp.GetReportId()})
}

func (s *Server) handleListBlocks(w http.ResponseWriter, r *http.Request, accountID string) {
	resp, err := s.moderation.ListBlocks(r.Context(), &moderationv1.ListBlocksRequest{AccountId: accountID})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		BlockedAccountIDs []string `json:"blocked_account_ids"`
	}{resp.GetBlockedAccountIds()})
}

func (s *Server) handleCreateBlock(w http.ResponseWriter, r *http.Request, accountID string) {
	var req struct {
		BlockedAccountID string `json:"blocked_account_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, status.Error(codes.InvalidArgument, "malformed request body"))
		return
	}
	resp, err := s.moderation.BlockUser(r.Context(), &moderationv1.BlockUserRequest{AccountId: accountID, BlockedAccountId: req.BlockedAccountID})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Blocked bool `json:"blocked"`
	}{resp.GetBlocked()})
}
