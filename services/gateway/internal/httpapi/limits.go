package httpapi

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
)

// Limits are the numbers behind the gateway's HTTP rate limits (DEC-0108).
// They exist to stop spam and abuse of the API, not to ration students, so
// they are set well above what a person does and are read from the
// environment (see main.go), letting them change without a code change.
type Limits struct {
	// GuestSignupsPerIPPerHour is per network address. Many students can sit
	// behind one campus address, so it is generous; it mainly slows one
	// machine from making accounts in a loop (for example to get around a ban).
	GuestSignupsPerIPPerHour int
	// GuestSignupsGlobalPerHour is the backstop across every address: if
	// something is creating accounts from many addresses at once, this stops
	// the flood. It is also the number to raise for a very busy launch hour.
	GuestSignupsGlobalPerHour int
	// CreateRoomPerWindow channels per account in CreateRoomWindow.
	CreateRoomPerWindow int
	CreateRoomWindow    time.Duration
	// JoinRoomPerMinute is per account; it makes guessing invite codes useless.
	JoinRoomPerMinute int
	// MaxChannelsGuest and MaxChannelsVerified cap how many group channels one
	// account can be in at once (creating and joining both count).
	MaxChannelsGuest    int
	MaxChannelsVerified int
}

// DefaultLimits are the production defaults.
func DefaultLimits() Limits {
	return Limits{
		GuestSignupsPerIPPerHour:  60,
		GuestSignupsGlobalPerHour: 1000,
		CreateRoomPerWindow:       10,
		CreateRoomWindow:          5 * time.Minute,
		JoinRoomPerMinute:         10,
		MaxChannelsGuest:          10,
		MaxChannelsVerified:       20,
	}
}

// WithLimits replaces the default limits.
func (s *Server) WithLimits(l Limits) *Server {
	s.limits = l
	return s
}

// checkChannelCap fails with ResourceExhausted when the account is already in
// as many group channels as its kind of account allows. A person counts as
// verified once they have linked Google.
func (s *Server) checkChannelCap(ctx context.Context, accountID string) error {
	acc, err := s.identity.GetAccount(ctx, &identityv1.GetAccountRequest{AccountId: accountID})
	if err != nil {
		return err
	}
	limit := s.limits.MaxChannelsGuest
	if acc.GetAccount().GetGoogleLinked() {
		limit = s.limits.MaxChannelsVerified
	}
	rooms, err := s.chat.ListMyRooms(ctx, &chatv1.ListMyRoomsRequest{AccountId: accountID})
	if err != nil {
		return err
	}
	groups := 0
	for _, r := range rooms.GetRooms() {
		if r.GetType() == chatv1.RoomType_ROOM_TYPE_GROUP {
			groups++
		}
	}
	if groups >= limit {
		return status.Error(codes.ResourceExhausted, fmt.Sprintf("you are already in %d channels, the most allowed. Leave one first", groups))
	}
	return nil
}
