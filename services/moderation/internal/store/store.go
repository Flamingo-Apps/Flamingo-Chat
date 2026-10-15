// Package store defines Moderation's persistence boundary for reports,
// blocks, bans, gender change requests and the admin action log.
// Business logic (internal/server) depends only on the Store interface,
// never on Postgres directly - the same pattern services/chat/internal/store
// and services/identity/internal/store use.
package store

import (
	"context"
	"errors"
	"time"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
)

// ErrNotFound is returned when a report, ban or gender change request
// lookup finds nothing.
var ErrNotFound = errors.New("not found")

type Report struct {
	ID                string
	ReporterAccountID string
	ReportedAccountID string
	RoomID            string
	Reason            string
	Resolved          bool
	ResolvedBy        string
	Outcome           string
	FiledAt           time.Time
	ResolvedAt        time.Time
}

type GenderChangeRequest struct {
	ID              string
	AccountID       string
	RequestedGender identityv1.Gender
	Note            string
	Resolved        bool
	Approved        bool
	ResolvedBy      string
	RequestedAt     time.Time
	ResolvedAt      time.Time
}

// AdminAction is one row in the admin action log (docs/ARCHITECTURE.md
// §6's "who, whom, why, when, outcome").
type AdminAction struct {
	ActorAccountID  string
	Action          string
	TargetAccountID string
	Detail          string
}

type Store interface {
	FileReport(ctx context.Context, id, reporterAccountID, reportedAccountID, roomID, reason string) (Report, error)
	// BlockUser is idempotent - blocking an already-blocked account
	// succeeds without creating a second row.
	BlockUser(ctx context.Context, accountID, blockedAccountID string) error
	ListBlocks(ctx context.Context, accountID string) ([]string, error)
	RequestGenderChange(ctx context.Context, id, accountID string, gender identityv1.Gender, note string) (GenderChangeRequest, error)

	ListReports(ctx context.Context, onlyUnresolved bool) ([]Report, error)
	// ResolveReport returns ErrNotFound if reportID doesn't exist.
	ResolveReport(ctx context.Context, reportID, resolvedBy, outcome string) error

	// BanAccount upserts: re-banning an account that was previously
	// unbanned reuses the same row rather than erroring or duplicating
	// it, so the row's own history stays in one place.
	BanAccount(ctx context.Context, accountID, reason, bannedBy string) error
	// UnbanAccount returns ErrNotFound if accountID has no ban row, and
	// is a no-op (not an error) if it's already unbanned.
	UnbanAccount(ctx context.Context, accountID, unbannedBy string) error

	ListGenderChangeRequests(ctx context.Context, onlyUnresolved bool) ([]GenderChangeRequest, error)
	// ResolveGenderChangeRequest returns ErrNotFound if requestID doesn't
	// exist. Approving it is recorded here; internal/server is
	// responsible for actually calling Identity.AdminSetGender - this
	// method only updates moderation's own record of the decision.
	ResolveGenderChangeRequest(ctx context.Context, requestID string, approve bool, resolvedBy string) error

	LogAdminAction(ctx context.Context, id string, action AdminAction) error
}
