package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"

	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
)

// fakeModeration is a minimal stand-in for cli.ModerationClient - just
// enough to prove dispatch routes to the right command with the right
// arguments, not to re-test internal/cli's own logic (cli_test.go
// already does that).
type fakeModeration struct {
	banAccount func(context.Context, *moderationv1.BanAccountRequest) (*moderationv1.BanAccountResponse, error)
}

func (f *fakeModeration) ListReports(context.Context, *moderationv1.ListReportsRequest, ...grpc.CallOption) (*moderationv1.ListReportsResponse, error) {
	return &moderationv1.ListReportsResponse{}, nil
}
func (f *fakeModeration) ResolveReport(context.Context, *moderationv1.ResolveReportRequest, ...grpc.CallOption) (*moderationv1.ResolveReportResponse, error) {
	return &moderationv1.ResolveReportResponse{}, nil
}
func (f *fakeModeration) BanAccount(ctx context.Context, req *moderationv1.BanAccountRequest, _ ...grpc.CallOption) (*moderationv1.BanAccountResponse, error) {
	if f.banAccount != nil {
		return f.banAccount(ctx, req)
	}
	return &moderationv1.BanAccountResponse{}, nil
}
func (f *fakeModeration) UnbanAccount(context.Context, *moderationv1.UnbanAccountRequest, ...grpc.CallOption) (*moderationv1.UnbanAccountResponse, error) {
	return &moderationv1.UnbanAccountResponse{}, nil
}
func (f *fakeModeration) ListGenderChangeRequests(context.Context, *moderationv1.ListGenderChangeRequestsRequest, ...grpc.CallOption) (*moderationv1.ListGenderChangeRequestsResponse, error) {
	return &moderationv1.ListGenderChangeRequestsResponse{}, nil
}
func (f *fakeModeration) ResolveGenderChangeRequest(context.Context, *moderationv1.ResolveGenderChangeRequestRequest, ...grpc.CallOption) (*moderationv1.ResolveGenderChangeRequestResponse, error) {
	return &moderationv1.ResolveGenderChangeRequestResponse{}, nil
}

func TestDispatch_Ban(t *testing.T) {
	var gotReq *moderationv1.BanAccountRequest
	client := &fakeModeration{banAccount: func(_ context.Context, req *moderationv1.BanAccountRequest) (*moderationv1.BanAccountResponse, error) {
		gotReq = req
		return &moderationv1.BanAccountResponse{}, nil
	}}
	var out bytes.Buffer
	err := dispatch(context.Background(), client, &out, []string{"ban", "-account=acc-1", "-reason=abuse", "-by=admin-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.GetAccountId() != "acc-1" || gotReq.GetReason() != "abuse" || gotReq.GetBannedBy() != "admin-1" {
		t.Fatalf("unexpected request: %+v", gotReq)
	}
}

func TestDispatch_BanMissingRequiredFlag(t *testing.T) {
	var out bytes.Buffer
	err := dispatch(context.Background(), &fakeModeration{}, &out, []string{"ban", "-account=acc-1"})
	if err == nil {
		t.Fatal("expected an error for a missing required flag")
	}
}

func TestDispatch_UnknownCommand(t *testing.T) {
	var out bytes.Buffer
	err := dispatch(context.Background(), &fakeModeration{}, &out, []string{"nonsense"})
	if err == nil {
		t.Fatal("expected an error for an unknown command")
	}
	if !strings.Contains(out.String(), "Usage") {
		t.Fatalf("expected usage to be printed, got %q", out.String())
	}
}

func TestDispatch_ReportsList(t *testing.T) {
	var out bytes.Buffer
	if err := dispatch(context.Background(), &fakeModeration{}, &out, []string{"reports", "list"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDispatch_ReportsUnknownSubcommand(t *testing.T) {
	var out bytes.Buffer
	if err := dispatch(context.Background(), &fakeModeration{}, &out, []string{"reports", "nonsense"}); err == nil {
		t.Fatal("expected an error for an unknown reports subcommand")
	}
}

func TestDispatch_GenderRequestsResolveRequiresExactlyOneFlag(t *testing.T) {
	var out bytes.Buffer
	err := dispatch(context.Background(), &fakeModeration{}, &out, []string{"gender-requests", "resolve", "-id=g1", "-by=admin-1"})
	if err == nil {
		t.Fatal("expected an error when neither -approve nor -reject is given")
	}

	err = dispatch(context.Background(), &fakeModeration{}, &out, []string{"gender-requests", "resolve", "-id=g1", "-by=admin-1", "-approve", "-reject"})
	if err == nil {
		t.Fatal("expected an error when both -approve and -reject are given")
	}
}

func TestDispatch_GenderRequestsResolve(t *testing.T) {
	var out bytes.Buffer
	err := dispatch(context.Background(), &fakeModeration{}, &out, []string{"gender-requests", "resolve", "-id=g1", "-by=admin-1", "-approve"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
