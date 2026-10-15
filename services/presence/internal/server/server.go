// Package server implements the PresenceService gRPC contract.
package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/presence/internal/store"
)

type Server struct {
	presencev1.UnimplementedPresenceServiceServer

	store store.Store
}

func New(s store.Store) *Server {
	return &Server{store: s}
}

func (s *Server) Heartbeat(ctx context.Context, req *presencev1.HeartbeatRequest) (*presencev1.HeartbeatResponse, error) {
	entries := make([]store.Entry, 0, len(req.GetEntries()))
	for _, e := range req.GetEntries() {
		entries = append(entries, store.Entry{AccountID: e.GetAccountId(), Category: e.GetCategory()})
	}
	if err := s.store.Record(ctx, entries); err != nil {
		return nil, status.Errorf(codes.Internal, "record heartbeat: %v", err)
	}
	return &presencev1.HeartbeatResponse{}, nil
}

func (s *Server) GetLiveCounts(ctx context.Context, _ *presencev1.GetLiveCountsRequest) (*presencev1.GetLiveCountsResponse, error) {
	counts, err := s.store.Counts(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get live counts: %v", err)
	}
	return &presencev1.GetLiveCountsResponse{
		Total: counts.Total, Guests: counts.Guests, Male: counts.Male, Female: counts.Female,
	}, nil
}
