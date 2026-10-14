package httpapi

import (
	"encoding/json"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
)

func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request, accountID string) {
	resp, err := s.identity.GetAccount(r.Context(), &identityv1.GetAccountRequest{AccountId: accountID})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAccountResponse(resp.GetAccount()))
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request, accountID string) {
	var req struct {
		Pseudonym string `json:"pseudonym"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, status.Error(codes.InvalidArgument, "malformed request body"))
		return
	}
	resp, err := s.identity.UpdatePseudonym(r.Context(), &identityv1.UpdatePseudonymRequest{
		AccountId: accountID, NewPseudonym: req.Pseudonym,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAccountResponse(resp.GetAccount()))
}

func (s *Server) handleSetGender(w http.ResponseWriter, r *http.Request, accountID string) {
	var req struct {
		Gender string `json:"gender"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, status.Error(codes.InvalidArgument, "malformed request body"))
		return
	}
	resp, err := s.identity.SetGender(r.Context(), &identityv1.SetGenderRequest{
		AccountId: accountID, Gender: genderFromString(req.Gender),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAccountResponse(resp.GetAccount()))
}

func (s *Server) handleRequestGenderChange(w http.ResponseWriter, r *http.Request, accountID string) {
	var req struct {
		RequestedGender string `json:"requested_gender"`
		Note            string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, status.Error(codes.InvalidArgument, "malformed request body"))
		return
	}
	resp, err := s.moderation.RequestGenderChange(r.Context(), &moderationv1.RequestGenderChangeRequest{
		AccountId: accountID, RequestedGender: genderFromString(req.RequestedGender), Note: req.Note,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		RequestID string `json:"request_id"`
	}{resp.GetRequestId()})
}
