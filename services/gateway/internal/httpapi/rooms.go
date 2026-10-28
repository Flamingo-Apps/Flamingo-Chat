package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
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
	// PartnerPseudonym is the other person's anonymous name in a direct
	// room, so a saved chat is listed under it (DEC-0106).
	PartnerPseudonym string `json:"partner_pseudonym,omitempty"`
	// PartnerAccountID lets the web app act on the other person (report them)
	// in a chat it restored from this list rather than from a live match_found.
	PartnerAccountID string `json:"partner_account_id,omitempty"`
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
		if room.GetType() == chatv1.RoomType_ROOM_TYPE_DIRECT {
			rooms[i].PartnerAccountID = partnerAccountID(room, accountID)
			rooms[i].PartnerPseudonym = s.partnerPseudonym(r, room, accountID)
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Rooms []roomResponse `json:"rooms"`
	}{rooms})
}

// partnerAccountID is the other member of a direct room, or "" when there is
// none to name.
func partnerAccountID(room *chatv1.Room, accountID string) string {
	for _, id := range room.GetMemberAccountIds() {
		if id != accountID {
			return id
		}
	}
	return ""
}

// partnerPseudonym is the anonymous name of the other member of a direct room,
// or "" when it cannot be found. A missing name must not fail the whole list:
// the web app falls back to a generic label.
func (s *Server) partnerPseudonym(r *http.Request, room *chatv1.Room, accountID string) string {
	for _, id := range room.GetMemberAccountIds() {
		if id == accountID {
			continue
		}
		resp, err := s.identity.GetAccount(r.Context(), &identityv1.GetAccountRequest{AccountId: id})
		if err != nil {
			return ""
		}
		return resp.GetAccount().GetPseudonym()
	}
	return ""
}

func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request, accountID string) {
	allowed, err := s.rateLimiter.Allow(r.Context(), "create_group_room", accountID, s.limits.CreateRoomPerWindow, s.limits.CreateRoomWindow)
	if err != nil {
		writeError(w, status.Errorf(codes.Internal, "rate limit: %v", err))
		return
	}
	if !allowed {
		writeError(w, status.Error(codes.ResourceExhausted, "you are creating channels too quickly, wait a few minutes"))
		return
	}
	if err := s.checkChannelCap(r.Context(), accountID); err != nil {
		writeError(w, err)
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
	// Without a limit, invite codes could be guessed in a loop.
	allowed, err := s.rateLimiter.Allow(r.Context(), "join_room", accountID, s.limits.JoinRoomPerMinute, time.Minute)
	if err != nil {
		writeError(w, status.Errorf(codes.Internal, "rate limit: %v", err))
		return
	}
	if !allowed {
		writeError(w, status.Error(codes.ResourceExhausted, "too many join attempts, wait a minute"))
		return
	}
	var req struct {
		InviteCode string `json:"invite_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, status.Error(codes.InvalidArgument, "malformed request body"))
		return
	}
	if err := s.checkChannelCap(r.Context(), accountID); err != nil {
		writeError(w, err)
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

// handleCloseRoom is "stop messaging" on a direct conversation: it stays in
// both Inboxes and stays readable, but nobody can send to it any more
// (DEC-0106).
func (s *Server) handleCloseRoom(w http.ResponseWriter, r *http.Request, accountID string) {
	roomID := r.PathValue("id")
	if _, err := s.chat.CloseDirectRoom(r.Context(), &chatv1.CloseDirectRoomRequest{RoomId: roomID, AccountId: accountID}); err != nil {
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
