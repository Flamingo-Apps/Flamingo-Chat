package server

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/presence/internal/store"
)

func TestHeartbeat_RecordsEveryEntry(t *testing.T) {
	s := &fakeStore{}
	srv := New(s)

	_, err := srv.Heartbeat(context.Background(), &presencev1.HeartbeatRequest{
		Entries: []*presencev1.HeartbeatEntry{
			{AccountId: "a", Category: presencev1.Category_CATEGORY_GUEST},
			{AccountId: "b", Category: presencev1.Category_CATEGORY_MALE},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.recorded) != 2 {
		t.Fatalf("expected 2 recorded entries, got %d", len(s.recorded))
	}
	if s.recorded[0] != (store.Entry{AccountID: "a", Category: presencev1.Category_CATEGORY_GUEST}) {
		t.Fatalf("unexpected first entry: %+v", s.recorded[0])
	}
}

func TestHeartbeat_StoreErrorBecomesInternal(t *testing.T) {
	s := &fakeStore{recordErr: errFake}
	srv := New(s)

	_, err := srv.Heartbeat(context.Background(), &presencev1.HeartbeatRequest{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("got %v, want Internal", err)
	}
}

func TestGetLiveCounts_ReturnsStoreCounts(t *testing.T) {
	s := &fakeStore{counts: store.Counts{Total: 10, Guests: 4, Male: 3, Female: 3}}
	srv := New(s)

	resp, err := srv.GetLiveCounts(context.Background(), &presencev1.GetLiveCountsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetTotal() != 10 || resp.GetGuests() != 4 || resp.GetMale() != 3 || resp.GetFemale() != 3 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestGetLiveCounts_StoreErrorBecomesInternal(t *testing.T) {
	s := &fakeStore{countsErr: errFake}
	srv := New(s)

	_, err := srv.GetLiveCounts(context.Background(), &presencev1.GetLiveCountsRequest{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("got %v, want Internal", err)
	}
}
