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

// VoteKeepGraceWindow is how long after a direct room ends (one side
// left before both voted to keep) the remaining member can still vote
// keep alone and have it count - see VoteKeep's doc comment (DEC-0091).
// The web client's own grace-window countdown (web/src/state.ts)
// matches this value; the two aren't sharable code across the Go/
// TypeScript boundary, so keep them in sync by hand if this changes.
const VoteKeepGraceWindow = 30 * time.Second

// ActiveDirectRoomWindow bounds how old an open, unsaved random chat can be
// and still count as "active" for ListRoomsForAccount. Chats abandoned by
// closing a tab are never ended (a known gap), so without a bound every one
// would be listed, and listened to, forever. A real chat lasts minutes.
const ActiveDirectRoomWindow = 12 * time.Hour

// ReplacedRoom is an open direct room that CreateDirectRoom ended because one
// of its members was matched into a new chat. MemberAccountID is that member
// (one of the new room's two), who is treated as the one who left it.
type ReplacedRoom struct {
	RoomID          string
	MemberAccountID string
}

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
	// EndedAt is nil while the room is still open. An ended room accepts no
	// more messages but can still be read.
	EndedAt *time.Time
	// MemberAccountIDs is everyone who is or ever was a member, filled in
	// only by ListRoomsForAccount, so a caller can tell who the other person
	// in a direct room is even after they left (DEC-0106).
	MemberAccountIDs []string
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
	//
	// In the same transaction it ends every other open, unsaved direct room
	// either member is still in, so a person is in at most one random chat at
	// a time (DEC-0112). Those rooms are returned so the caller can tell the
	// people left behind. A saved (kept) chat is never touched.
	CreateDirectRoom(ctx context.Context, id string, memberAccountIDs []string) (Room, []ReplacedRoom, error)

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

	// MembershipState says whether an account is a current member of a room
	// and whether that room is still open (not ended), in one query: what
	// SendMessage needs on every message.
	MembershipState(ctx context.Context, roomID, accountID string) (isMember, roomOpen bool, err error)

	// ListRoomsForAccount returns the rooms an account is a current member
	// of that matter to it (DEC-0106): every group room; a direct room the
	// account voted to keep (so the person who skipped a chat does not get
	// it in their Inbox just because they were once in it); and a direct
	// room that is still open, unsaved and recent, so that a connection
	// that reconnects can listen to a random chat in progress again. Each
	// room carries the ids of everyone who is or was in it.
	ListRoomsForAccount(ctx context.Context, accountID string) ([]Room, error)

	// VoteKeep records a keep vote for a direct room. Idempotent: voting
	// again does not count twice. Returns true once every current member
	// has voted, at which point the room's Kept flag is also set to true
	// (DEC-0031). If the room has already ended (one side left before
	// both voted) and it ended within VoteKeepGraceWindow, a single vote
	// from whoever is left is enough on its own - the other side is
	// confirmed gone and can never complete a mutual vote (DEC-0091).
	VoteKeep(ctx context.Context, roomID, accountID string) (kept bool, err error)

	// EndDirectRoom marks a direct room as ended, unless it is already
	// kept (a kept room stays open regardless of who leaves). Idempotent:
	// ending an already-ended room is not an error.
	EndDirectRoom(ctx context.Context, roomID string) error

	// CloseDirectRoom marks a direct room as ended whether or not it was
	// kept: a saved chat is closed to further messages but stays readable.
	// Idempotent.
	CloseDirectRoom(ctx context.Context, roomID string) error

	// ListMessages returns up to limit messages in a room older than
	// beforeMessageID (newest first), or the newest page if
	// beforeMessageID is empty.
	ListMessages(ctx context.Context, roomID, beforeMessageID string, limit int) ([]Message, error)
}
