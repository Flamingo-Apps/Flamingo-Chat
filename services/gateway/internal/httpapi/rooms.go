package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
)

type roomResponse struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Name           string `json:"name,omitempty"`
	OwnerAccountID string `json:"owner_account_id,omitempty"`
	InviteCode     string `json:"invite_code,omitempty"`
	Kept           bool   `json:"kept"`
	CreatedAtUnix  int64  `json:"created_at_unix"`
	EndedAtUnix    int64  `json:"ended_at_unix,omitempty"`
}

func toRoomResponse(r *chatv1.Room) roomResponse {
	roomType := "direct"
	if r.GetType() == chatv1.RoomType_ROOM_TYPE_GROUP {
		roomType = "group"
	}
	return roomResponse{
		ID:             r.GetId(),
		Type:           roomType,
		Name:           r.GetName(),
		OwnerAccountID: r.GetOwnerAccountId(),
		InviteCode:     r.GetInviteCode(),
		Kept:           r.GetKept(),
		CreatedAtUnix:  r.GetCreatedAtUnix(),
		EndedAtUnix:    r.GetEndedAtUnix(),
	}
}

type messageResponse struct {
	MessageID       string `json:"message_id"`
	RoomID          string `json:"room_id"`
	SenderAccountID string `json:"sender_account_id"`
	SenderPseudonym string `json:"sender_pseudonym"`
	Body            string `json:"body"`
	SentAtUnix      int64  `json:"sent_at_unix"`
}

func toMessageResponse(m *chatv1.Message) messageResponse {
	return messageResponse{
		MessageID:       m.GetMessageId(),
		RoomID:          m.GetRoomId(),
		SenderAccountID: m.GetSenderAccountId(),
		SenderPseudonym: m.GetSenderPseudonym(),
		Body:            m.GetBody(),
		SentAtUnix:      m.GetSentAtUnix(),
	}
}

func (s *Server) handleListRooms(w http.ResponseWriter, r *http.Request, accountID string) {
	resp, err := s.chat.ListMyRooms(r.Context(), &chatv1.ListMyRoomsRequest{AccountId: accountID})
	if err != nil {
		writeError(w, err)
		return
	}
	rooms := make([]roomResponse, len(resp.GetRooms()))
	for i, room := range resp.GetRooms() {
		rooms[i] = toRoomResponse(room)
	}
	writeJSON(w, http.StatusOK, struct {
		Rooms []roomResponse `json:"rooms"`
	}{rooms})
}

func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request, accountID string) {
	allowed, err := s.rateLimiter.Allow(r.Context(), "create_group_room", accountID, 3, 24*time.Hour)
	if err != nil {
		writeError(w, status.Errorf(codes.Internal, "rate limit: %v", err))
		return
	}
	if !allowed {
		writeError(w, status.Error(codes.ResourceExhausted, "too many rooms created today"))
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, status.Error(codes.InvalidArgument, "malformed request body"))
		return
	}
	resp, err := s.chat.CreateGroupRoom(r.Context(), &chatv1.CreateGroupRoomRequest{OwnerAccountId: accountID, Name: req.Name})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRoomResponse(resp.GetRoom()))
}

func (s *Server) handleJoinRoom(w http.ResponseWriter, r *http.Request, accountID string) {
	var req struct {
		InviteCode string `json:"invite_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, status.Error(codes.InvalidArgument, "malformed request body"))
		return
	}
	resp, err := s.chat.JoinRoomByCode(r.Context(), &chatv1.JoinRoomByCodeRequest{AccountId: accountID, InviteCode: req.InviteCode})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRoomResponse(resp.GetRoom()))
}

func (s *Server) handleLeaveRoom(w http.ResponseWriter, r *http.Request, accountID string) {
	roomID := r.PathValue("id")
	if _, err := s.chat.LeaveRoom(r.Context(), &chatv1.LeaveRoomRequest{RoomId: roomID, AccountId: accountID}); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRotateInviteCode(w http.ResponseWriter, r *http.Request, accountID string) {
	roomID := r.PathValue("id")
	resp, err := s.chat.RotateInviteCode(r.Context(), &chatv1.RotateInviteCodeRequest{RoomId: roomID, RequestedByAccountId: accountID})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		InviteCode string `json:"invite_code"`
	}{resp.GetInviteCode()})
}

// maxMessagesLimit bounds the ?limit= query parameter before it's ever
// cast to the int32 the ListMessages RPC takes - Chat's own handler
// clamps an out-of-range value too, but parsing an arbitrarily large
// query parameter straight into int32 without a bound of our own here
// risks a silent overflow before the request even leaves the gateway.
const maxMessagesLimit = 200

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request, accountID string) {
	roomID := r.PathValue("id")
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 0 {
		limit = 0
	}
	if limit > maxMessagesLimit {
		limit = maxMessagesLimit
	}
	resp, err := s.chat.ListMessages(r.Context(), &chatv1.ListMessagesRequest{
		RoomId: roomID, AccountId: accountID,
		BeforeMessageId: r.URL.Query().Get("before"),
		//nolint:gosec // clamped to [0, maxMessagesLimit] just above - gosec can't see across the bounds check
		Limit: int32(limit),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	messages := make([]messageResponse, len(resp.GetMessages()))
	for i, m := range resp.GetMessages() {
		messages[i] = toMessageResponse(m)
	}
	writeJSON(w, http.StatusOK, struct {
		Messages []messageResponse `json:"messages"`
	}{messages})
}
