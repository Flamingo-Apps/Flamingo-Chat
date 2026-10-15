package cli

import (
	"context"
	"errors"

	"google.golang.org/grpc"

	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
)

var errNotConfigured = errors.New("fake: not configured")

// fakeModeration implements ModerationClient. Each field defaults to
// failing with errNotConfigured, so a test only has to configure the
// one method it exercises - the same pattern the services' own
// fakes_test.go files use.
type fakeModeration struct {
	listReports                func(context.Context, *moderationv1.ListReportsRequest) (*moderationv1.ListReportsResponse, error)
	resolveReport              func(context.Context, *moderationv1.ResolveReportRequest) (*moderationv1.ResolveReportResponse, error)
	banAccount                 func(context.Context, *moderationv1.BanAccountRequest) (*moderationv1.BanAccountResponse, error)
	unbanAccount               func(context.Context, *moderationv1.UnbanAccountRequest) (*moderationv1.UnbanAccountResponse, error)
	listGenderChangeRequests   func(context.Context, *moderationv1.ListGenderChangeRequestsRequest) (*moderationv1.ListGenderChangeRequestsResponse, error)
	resolveGenderChangeRequest func(context.Context, *moderationv1.ResolveGenderChangeRequestRequest) (*moderationv1.ResolveGenderChangeRequestResponse, error)
}

func (f *fakeModeration) ListReports(ctx context.Context, req *moderationv1.ListReportsRequest, _ ...grpc.CallOption) (*moderationv1.ListReportsResponse, error) {
	if f.listReports == nil {
		return nil, errNotConfigured
	}
	return f.listReports(ctx, req)
}

func (f *fakeModeration) ResolveReport(ctx context.Context, req *moderationv1.ResolveReportRequest, _ ...grpc.CallOption) (*moderationv1.ResolveReportResponse, error) {
	if f.resolveReport == nil {
		return nil, errNotConfigured
	}
	return f.resolveReport(ctx, req)
}

func (f *fakeModeration) BanAccount(ctx context.Context, req *moderationv1.BanAccountRequest, _ ...grpc.CallOption) (*moderationv1.BanAccountResponse, error) {
	if f.banAccount == nil {
		return nil, errNotConfigured
	}
	return f.banAccount(ctx, req)
}

func (f *fakeModeration) UnbanAccount(ctx context.Context, req *moderationv1.UnbanAccountRequest, _ ...grpc.CallOption) (*moderationv1.UnbanAccountResponse, error) {
	if f.unbanAccount == nil {
		return nil, errNotConfigured
	}
	return f.unbanAccount(ctx, req)
}

func (f *fakeModeration) ListGenderChangeRequests(ctx context.Context, req *moderationv1.ListGenderChangeRequestsRequest, _ ...grpc.CallOption) (*moderationv1.ListGenderChangeRequestsResponse, error) {
	if f.listGenderChangeRequests == nil {
		return nil, errNotConfigured
	}
	return f.listGenderChangeRequests(ctx, req)
}

func (f *fakeModeration) ResolveGenderChangeRequest(ctx context.Context, req *moderationv1.ResolveGenderChangeRequestRequest, _ ...grpc.CallOption) (*moderationv1.ResolveGenderChangeRequestResponse, error) {
	if f.resolveGenderChangeRequest == nil {
		return nil, errNotConfigured
	}
	return f.resolveGenderChangeRequest(ctx, req)
}
