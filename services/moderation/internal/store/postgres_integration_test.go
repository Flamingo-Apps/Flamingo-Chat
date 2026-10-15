//go:build integration

// Run with POSTGRES_URL pointing at a real Postgres:
//
//	go test -tags integration ./internal/store/...
package store

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/moderation/migrations"
)

func newTestPostgres(t *testing.T) *Postgres {
	t.Helper()
	ctx := context.Background()

	url := "postgres://flamingo:flamingo@localhost:5432/flamingo?sslmode=disable"
	if v := os.Getenv("POSTGRES_URL"); v != "" {
		url = v
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	sqlDB := stdlib.OpenDBFromPool(pool)
	if err := migrations.Run(sqlDB); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("closing migration db handle: %v", err)
	}

	if _, err := pool.Exec(ctx, "TRUNCATE admin_actions, gender_change_requests, bans, blocks, reports"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return NewPostgres(pool)
}

func TestFileReport_RoundTrips(t *testing.T) {
	s := newTestPostgres(t)
	ctx := context.Background()
	id := uuid.NewString()
	reporter, reported, room := uuid.NewString(), uuid.NewString(), uuid.NewString()

	r, err := s.FileReport(ctx, id, reporter, reported, room, "spam")
	if err != nil {
		t.Fatalf("FileReport: %v", err)
	}
	if r.ID != id || r.ReporterAccountID != reporter || r.ReportedAccountID != reported || r.RoomID != room || r.Reason != "spam" || r.Resolved {
		t.Fatalf("unexpected report: %+v", r)
	}
}

func TestFileReport_EmptyRoomID(t *testing.T) {
	s := newTestPostgres(t)
	ctx := context.Background()
	r, err := s.FileReport(ctx, uuid.NewString(), uuid.NewString(), uuid.NewString(), "", "spam")
	if err != nil {
		t.Fatalf("FileReport: %v", err)
	}
	if r.RoomID != "" {
		t.Fatalf("expected empty room id, got %q", r.RoomID)
	}
}

func TestBlockUser_IsIdempotent(t *testing.T) {
	s := newTestPostgres(t)
	ctx := context.Background()
	a, b := uuid.NewString(), uuid.NewString()

	if err := s.BlockUser(ctx, a, b); err != nil {
		t.Fatalf("first BlockUser: %v", err)
	}
	if err := s.BlockUser(ctx, a, b); err != nil {
		t.Fatalf("second BlockUser: %v", err)
	}
	ids, err := s.ListBlocks(ctx, a)
	if err != nil {
		t.Fatalf("ListBlocks: %v", err)
	}
	if len(ids) != 1 || ids[0] != b {
		t.Fatalf("expected exactly one block, got %+v", ids)
	}
}

func TestResolveReport_NotFound(t *testing.T) {
	s := newTestPostgres(t)
	if err := s.ResolveReport(context.Background(), uuid.NewString(), uuid.NewString(), "outcome"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestResolveReport_RoundTrips(t *testing.T) {
	s := newTestPostgres(t)
	ctx := context.Background()
	id := uuid.NewString()
	if _, err := s.FileReport(ctx, id, uuid.NewString(), uuid.NewString(), "", "spam"); err != nil {
		t.Fatalf("FileReport: %v", err)
	}
	resolver := uuid.NewString()
	if err := s.ResolveReport(ctx, id, resolver, "warned"); err != nil {
		t.Fatalf("ResolveReport: %v", err)
	}
	reports, err := s.ListReports(ctx, false)
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	var found bool
	for _, r := range reports {
		if r.ID == id {
			found = true
			if !r.Resolved || r.ResolvedBy != resolver || r.Outcome != "warned" {
				t.Fatalf("unexpected resolved report: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("expected to find the resolved report")
	}
}

func TestListReports_OnlyUnresolvedFilters(t *testing.T) {
	s := newTestPostgres(t)
	ctx := context.Background()
	resolvedID, unresolvedID := uuid.NewString(), uuid.NewString()
	if _, err := s.FileReport(ctx, resolvedID, uuid.NewString(), uuid.NewString(), "", "r1"); err != nil {
		t.Fatalf("FileReport: %v", err)
	}
	if _, err := s.FileReport(ctx, unresolvedID, uuid.NewString(), uuid.NewString(), "", "r2"); err != nil {
		t.Fatalf("FileReport: %v", err)
	}
	if err := s.ResolveReport(ctx, resolvedID, uuid.NewString(), "done"); err != nil {
		t.Fatalf("ResolveReport: %v", err)
	}

	reports, err := s.ListReports(ctx, true)
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	for _, r := range reports {
		if r.ID == resolvedID {
			t.Fatalf("expected the resolved report to be excluded, got %+v", r)
		}
	}
}

func TestBanAccount_UpsertsAndClearsUnbanFields(t *testing.T) {
	s := newTestPostgres(t)
	ctx := context.Background()
	account := uuid.NewString()

	if err := s.BanAccount(ctx, account, "first reason", uuid.NewString()); err != nil {
		t.Fatalf("BanAccount: %v", err)
	}
	if err := s.UnbanAccount(ctx, account, uuid.NewString()); err != nil {
		t.Fatalf("UnbanAccount: %v", err)
	}
	// Re-banning after an unban should reuse the same row, clearing the
	// unban fields back out.
	if err := s.BanAccount(ctx, account, "second reason", uuid.NewString()); err != nil {
		t.Fatalf("re-BanAccount: %v", err)
	}
	// A second UnbanAccount should succeed (idempotent) since the account
	// is banned again at this point.
	if err := s.UnbanAccount(ctx, account, uuid.NewString()); err != nil {
		t.Fatalf("second UnbanAccount: %v", err)
	}
}

func TestUnbanAccount_NotFound(t *testing.T) {
	s := newTestPostgres(t)
	if err := s.UnbanAccount(context.Background(), uuid.NewString(), uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestGenderChangeRequest_RoundTripsAndResolves(t *testing.T) {
	s := newTestPostgres(t)
	ctx := context.Background()
	id := uuid.NewString()
	account := uuid.NewString()

	g, err := s.RequestGenderChange(ctx, id, account, identityv1.Gender_GENDER_FEMALE, "please")
	if err != nil {
		t.Fatalf("RequestGenderChange: %v", err)
	}
	if g.AccountID != account || g.RequestedGender != identityv1.Gender_GENDER_FEMALE || g.Note != "please" || g.Resolved {
		t.Fatalf("unexpected request: %+v", g)
	}

	resolver := uuid.NewString()
	if err := s.ResolveGenderChangeRequest(ctx, id, true, resolver); err != nil {
		t.Fatalf("ResolveGenderChangeRequest: %v", err)
	}
	requests, err := s.ListGenderChangeRequests(ctx, false)
	if err != nil {
		t.Fatalf("ListGenderChangeRequests: %v", err)
	}
	var found bool
	for _, r := range requests {
		if r.ID == id {
			found = true
			if !r.Resolved || !r.Approved || r.ResolvedBy != resolver {
				t.Fatalf("unexpected resolved request: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("expected to find the resolved request")
	}
}

func TestResolveGenderChangeRequest_NotFound(t *testing.T) {
	s := newTestPostgres(t)
	if err := s.ResolveGenderChangeRequest(context.Background(), uuid.NewString(), true, uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestLogAdminAction_RoundTrips(t *testing.T) {
	s := newTestPostgres(t)
	ctx := context.Background()
	actor, target := uuid.NewString(), uuid.NewString()
	if err := s.LogAdminAction(ctx, uuid.NewString(), AdminAction{
		ActorAccountID: actor, Action: "ban_account", TargetAccountID: target, Detail: "abuse",
	}); err != nil {
		t.Fatalf("LogAdminAction: %v", err)
	}
	// No reader RPC exists for admin_actions yet (nothing in
	// moderation.proto lists them) - this test only proves the insert
	// itself succeeds against the real schema (foreign-key-free, but
	// column types and NOT NULL constraints are real).
}
