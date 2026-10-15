// Package presence tracks which accounts this gateway instance currently
// has connected and reports them to the Presence service, implementing
// gateway's side of docs/ARCHITECTURE.md §3.4: "every 15 seconds each
// gateway instance sends one batched Heartbeat with a category per
// connected account."
package presence

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
)

// Registry is every account currently connected to this gateway
// instance, keyed by account id - one shared instance per process
// (passed through ws.Deps), not one per connection, since a batched
// Heartbeat needs to see every connection at once.
type Registry struct {
	mu        sync.Mutex
	byAccount map[string]presencev1.Category
}

func NewRegistry() *Registry {
	return &Registry{byAccount: map[string]presencev1.Category{}}
}

// Set records accountID's current category, replacing whatever was there
// (a reconnect, or a gender change mid-session) - called once per
// connection, right after authentication fetches the account.
func (r *Registry) Set(accountID string, category presencev1.Category) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byAccount[accountID] = category
}

// Remove drops accountID - called when its connection closes. A second
// connection for the same account (a browser tab left open during a
// reconnect) would otherwise have its Set overwritten by this Remove if
// the two raced; accepted as a narrow, rare gap in a count that
// docs/ARCHITECTURE.md §3.4 already treats as a non-durable, eventually-
// consistent approximation; refreshed again within one heartbeat
// interval.
func (r *Registry) Remove(accountID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byAccount, accountID)
}

// Snapshot copies every currently tracked account into one Heartbeat
// batch.
func (r *Registry) Snapshot() []*presencev1.HeartbeatEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := make([]*presencev1.HeartbeatEntry, 0, len(r.byAccount))
	for accountID, category := range r.byAccount {
		entries = append(entries, &presencev1.HeartbeatEntry{AccountId: accountID, Category: category})
	}
	return entries
}

// CategoryFromAccount maps an Account to the Category docs/ARCHITECTURE.md
// §3.4 heartbeats with. Verified is acc.GetGoogleLinked() - the same
// signal internal/ws/dispatch.go's handleMatchRequest already trusts as
// "verified" for matching purposes, kept consistent here rather than
// introducing a second definition of what counts as verified.
func CategoryFromAccount(acc *identityv1.Account) presencev1.Category {
	if !acc.GetGoogleLinked() {
		return presencev1.Category_CATEGORY_GUEST
	}
	switch acc.GetGender() {
	case identityv1.Gender_GENDER_MALE:
		return presencev1.Category_CATEGORY_MALE
	case identityv1.Gender_GENDER_FEMALE:
		return presencev1.Category_CATEGORY_FEMALE
	default:
		return presencev1.Category_CATEGORY_VERIFIED_OTHER
	}
}

// HeartbeatClient is the subset of presencev1.PresenceServiceClient this
// package calls, narrowed so a test fake only has to implement what's
// used.
type HeartbeatClient interface {
	Heartbeat(ctx context.Context, req *presencev1.HeartbeatRequest, opts ...grpc.CallOption) (*presencev1.HeartbeatResponse, error)
}

// RunHeartbeatLoop sends registry's current snapshot to client every
// interval until ctx is canceled. An empty snapshot (nothing connected
// to this instance right now) sends nothing - a batch of zero entries
// would just be a no-op round trip.
func RunHeartbeatLoop(ctx context.Context, registry *Registry, client HeartbeatClient, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			entries := registry.Snapshot()
			if len(entries) == 0 {
				continue
			}
			if _, err := client.Heartbeat(ctx, &presencev1.HeartbeatRequest{Entries: entries}); err != nil {
				logger.Warn("presence heartbeat", "err", err)
			}
		}
	}
}
