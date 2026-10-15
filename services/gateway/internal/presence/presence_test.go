package presence

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
)

func TestCategoryFromAccount(t *testing.T) {
	cases := []struct {
		name string
		acc  *identityv1.Account
		want presencev1.Category
	}{
		{"unverified guest", &identityv1.Account{GoogleLinked: false, Gender: identityv1.Gender_GENDER_MALE}, presencev1.Category_CATEGORY_GUEST},
		{"verified male", &identityv1.Account{GoogleLinked: true, Gender: identityv1.Gender_GENDER_MALE}, presencev1.Category_CATEGORY_MALE},
		{"verified female", &identityv1.Account{GoogleLinked: true, Gender: identityv1.Gender_GENDER_FEMALE}, presencev1.Category_CATEGORY_FEMALE},
		{"verified unspecified", &identityv1.Account{GoogleLinked: true, Gender: identityv1.Gender_GENDER_UNSPECIFIED}, presencev1.Category_CATEGORY_VERIFIED_OTHER},
		{"verified prefers not to say", &identityv1.Account{GoogleLinked: true, Gender: identityv1.Gender_GENDER_PREFER_NOT_TO_SAY}, presencev1.Category_CATEGORY_VERIFIED_OTHER},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CategoryFromAccount(tc.acc); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRegistry_SetSnapshotRemove(t *testing.T) {
	r := NewRegistry()
	r.Set("a", presencev1.Category_CATEGORY_GUEST)
	r.Set("b", presencev1.Category_CATEGORY_MALE)

	entries := r.Snapshot()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	r.Remove("a")
	entries = r.Snapshot()
	if len(entries) != 1 || entries[0].GetAccountId() != "b" {
		t.Fatalf("expected only b to remain, got %+v", entries)
	}
}

func TestRegistry_SetOverwritesExisting(t *testing.T) {
	r := NewRegistry()
	r.Set("a", presencev1.Category_CATEGORY_GUEST)
	r.Set("a", presencev1.Category_CATEGORY_MALE)

	entries := r.Snapshot()
	if len(entries) != 1 || entries[0].GetCategory() != presencev1.Category_CATEGORY_MALE {
		t.Fatalf("expected a's category to be overwritten, got %+v", entries)
	}
}

type fakeHeartbeatClient struct {
	calls [][]*presencev1.HeartbeatEntry
	err   error
}

func (f *fakeHeartbeatClient) Heartbeat(_ context.Context, req *presencev1.HeartbeatRequest, _ ...grpc.CallOption) (*presencev1.HeartbeatResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.calls = append(f.calls, req.GetEntries())
	return &presencev1.HeartbeatResponse{}, nil
}

func TestRunHeartbeatLoop_SendsNonEmptySnapshots(t *testing.T) {
	r := NewRegistry()
	r.Set("a", presencev1.Category_CATEGORY_GUEST)
	client := &fakeHeartbeatClient{}

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
	defer cancel()
	RunHeartbeatLoop(ctx, r, client, 10*time.Millisecond, slog.Default())

	if len(client.calls) == 0 {
		t.Fatal("expected at least one Heartbeat call")
	}
	if len(client.calls[0]) != 1 || client.calls[0][0].GetAccountId() != "a" {
		t.Fatalf("unexpected entries: %+v", client.calls[0])
	}
}

func TestRunHeartbeatLoop_SkipsEmptySnapshot(t *testing.T) {
	r := NewRegistry()
	client := &fakeHeartbeatClient{}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	RunHeartbeatLoop(ctx, r, client, 10*time.Millisecond, slog.Default())

	if len(client.calls) != 0 {
		t.Fatalf("expected no Heartbeat calls with nothing registered, got %d", len(client.calls))
	}
}
