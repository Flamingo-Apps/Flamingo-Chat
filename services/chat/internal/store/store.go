// Package store defines Chat's persistence boundary for rooms,
// membership and keep votes. Business logic (internal/server) depends
// only on the RoomStore interface, never on Postgres directly - the same
// pattern services/identity/internal/store uses.
//
// Messages are deliberately not written here. Chat's SendMessage only
// produces a MessageSent event to Kafka (docs/ARCHITECTURE.md §3.1,
// DEC-0047); the persistence worker - not yet built, see T6 in
// .claude/build-plan.md - is what actually inserts into the messages
// table this package reads from. ListMessages will see nothing newer
// than whatever the persistence worker has caught up to.
package store

import (
	"context"
	"errors"
	"time"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
)

// ErrNotFound is returned when a room lookup finds nothing.
var ErrNotFound = errors.New("room not found")

// ErrAlreadyExists is returned on an invite code collision.
var ErrAlreadyExists = errors.New("invite code already exists")

// Room is Chat's persistence-layer view of a room. Deliberately separate
// from the generated chatv1.Room protobuf type, the same separation
// identity's store.Account keeps from identityv1.Account.
type Room struct {
	ID   string
	Type chatv1.RoomType
	// Name and OwnerAccountID are set for group rooms only.
	Name           *string
	OwnerAccountID *string
	// InviteCode is set for group rooms only.
	InviteCode *string
	// Kept is meaningful for direct rooms only (DEC-0031).
	Kept      bool
	CreatedAt time.Time
	// EndedAt is nil while the room is still open.
	EndedAt *time.Time
}

// Message is Chat's persistence-layer view of a message row, as written
// by the persistence worker.
type Message struct {
	MessageID       string
	RoomID          string
	SenderAccountID string
	SenderPseudonym string
	Body            string
	SentAt          time.Time
}

// RoomStore is the persistence boundary for rooms, membership and keep
// votes.
type RoomStore interface {
	// CreateGroupRoom creates a new group room with the given owner, name
	// and invite code. Returns ErrAlreadyExists if the invite code
	// collides with an existing room's.
	CreateGroupRoom(ctx context.Context, id, ownerAccountID, name, inviteCode string) (Room, error)

	// CreateDirectRoom creates a new direct room with exactly the given
	// members already joined, as one atomic operation - called only by
	// Matching when it pairs two users (DEC-0044).
	CreateDirectRoom(ctx context.Context, id string, memberAccountIDs []string) (Room, error)

	// GetRoom fetches a room by ID. Returns ErrNotFound if it doesn't
	// exist.
	GetRoom(ctx context.Context, id string) (Room, error)

	// GetRoomByInviteCode fetches a group room by its current invite
	// code. Returns ErrNotFound if no room has this code.
	GetRoomByInviteCode(ctx context.Context, inviteCode string) (Room, error)

	// RotateInviteCode replaces a group room's invite code. The caller is
	// responsible for checking the requester is the room's owner before
	// calling this - that is business logic, not a storage concern.
	// Returns ErrNotFound if the room doesn't exist, ErrAlreadyExists if
	// the new code collides with another room's.
	RotateInviteCode(ctx context.Context, roomID, newInviteCode string) (Room, error)

	// AddMember adds an account as a current member of a room, or
	// rejoins a former member by clearing their left_at. Idempotent: it
	// is not an error to add a current member again.
	AddMember(ctx context.Context, roomID, accountID string) error

	// RemoveMember marks an account as no longer a current member of a
	// room. Idempotent: it is not an error to remove a former or
	// never-was member.
	RemoveMember(ctx context.Context, roomID, accountID string) error

	// IsCurrentMember reports whether an account is a current (not left)
	// member of a room.
	IsCurrentMember(ctx context.Context, roomID, accountID string) (bool, error)

	// HasEverBeenMember reports whether an account is or ever was a
	// member of a room - what ListMessages checks, so a former member
	// can still see history from while they were in the room.
	HasEverBeenMember(ctx context.Context, roomID, accountID string) (bool, error)

	// ListRoomsForAccount returns every room where the account is a
	// current member and the room is either a group room or a kept
	// direct room (docs/ARCHITECTURE.md §5.1's "my group rooms and kept
	// conversations").
	ListRoomsForAccount(ctx context.Context, accountID string) ([]Room, error)

	// VoteKeep records a keep vote for a direct room. Idempotent: voting
	// again does not count twice. Returns true once every current member
	// has voted, at which point the room's Kept flag is also set to true
	// (DEC-0031).
	VoteKeep(ctx context.Context, roomID, accountID string) (kept bool, err error)

	// EndDirectRoom marks a direct room as ended, unless it is already
	// kept (a kept room stays open regardless of who leaves). Idempotent:
	// ending an already-ended room is not an error.
	EndDirectRoom(ctx context.Context, roomID string) error

	// ListMessages returns up to limit messages in a room older than
	// beforeMessageID (newest first), or the newest page if
	// beforeMessageID is empty.
	ListMessages(ctx context.Context, roomID, beforeMessageID string, limit int) ([]Message, error)
}
