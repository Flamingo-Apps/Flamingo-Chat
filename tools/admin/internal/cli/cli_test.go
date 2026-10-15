package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
)

func TestListReports_PrintsEachReport(t *testing.T) {
	client := &fakeModeration{
		listReports: func(_ context.Context, req *moderationv1.ListReportsRequest) (*moderationv1.ListReportsResponse, error) {
			if !req.GetOnlyUnresolved() {
				t.Fatalf("expected OnlyUnresolved to be passed through as true")
			}
			return &moderationv1.ListReportsResponse{Reports: []*moderationv1.Report{
				{Id: "r1", ReporterAccountId: "a", ReportedAccountId: "b", Reason: "spam"},
			}}, nil
		},
	}
	var out bytes.Buffer
	if err := ListReports(context.Background(), client, &out, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "r1") || !strings.Contains(out.String(), "spam") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestListReports_NoneFound(t *testing.T) {
	client := &fakeModeration{
		listReports: func(context.Context, *moderationv1.ListReportsRequest) (*moderationv1.ListReportsResponse, error) {
			return &moderationv1.ListReportsResponse{}, nil
		},
	}
	var out bytes.Buffer
	if err := ListReports(context.Background(), client, &out, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "no reports") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestListReports_PropagatesError(t *testing.T) {
	client := &fakeModeration{}
	var out bytes.Buffer
	if err := ListReports(context.Background(), client, &out, false); err == nil {
		t.Fatal("expected an error")
	}
}

func TestResolveReport(t *testing.T) {
	var gotReq *moderationv1.ResolveReportRequest
	client := &fakeModeration{
		resolveReport: func(_ context.Context, req *moderationv1.ResolveReportRequest) (*moderationv1.ResolveReportResponse, error) {
			gotReq = req
			return &moderationv1.ResolveReportResponse{}, nil
		},
	}
	var out bytes.Buffer
	if err := ResolveReport(context.Background(), client, &out, "r1", "admin-1", "warned"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.GetReportId() != "r1" || gotReq.GetResolvedBy() != "admin-1" || gotReq.GetOutcome() != "warned" {
		t.Fatalf("unexpected request: %+v", gotReq)
	}
	if !strings.Contains(out.String(), "r1") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestBanAccount(t *testing.T) {
	var gotReq *moderationv1.BanAccountRequest
	client := &fakeModeration{
		banAccount: func(_ context.Context, req *moderationv1.BanAccountRequest) (*moderationv1.BanAccountResponse, error) {
			gotReq = req
			return &moderationv1.BanAccountResponse{}, nil
		},
	}
	var out bytes.Buffer
	if err := BanAccount(context.Background(), client, &out, "acc-1", "abuse", "admin-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.GetAccountId() != "acc-1" || gotReq.GetReason() != "abuse" || gotReq.GetBannedBy() != "admin-1" {
		t.Fatalf("unexpected request: %+v", gotReq)
	}
	if !strings.Contains(out.String(), "acc-1") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestUnbanAccount(t *testing.T) {
	var gotReq *moderationv1.UnbanAccountRequest
	client := &fakeModeration{
		unbanAccount: func(_ context.Context, req *moderationv1.UnbanAccountRequest) (*moderationv1.UnbanAccountResponse, error) {
			gotReq = req
			return &moderationv1.UnbanAccountResponse{}, nil
		},
	}
	var out bytes.Buffer
	if err := UnbanAccount(context.Background(), client, &out, "acc-1", "admin-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.GetAccountId() != "acc-1" || gotReq.GetUnbannedBy() != "admin-1" {
		t.Fatalf("unexpected request: %+v", gotReq)
	}
}

func TestListGenderChangeRequests_PrintsEach(t *testing.T) {
	client := &fakeModeration{
		listGenderChangeRequests: func(context.Context, *moderationv1.ListGenderChangeRequestsRequest) (*moderationv1.ListGenderChangeRequestsResponse, error) {
			return &moderationv1.ListGenderChangeRequestsResponse{Requests: []*moderationv1.GenderChangeRequest{
				{Id: "g1", AccountId: "a", RequestedGender: identityv1.Gender_GENDER_FEMALE, Note: "please"},
			}}, nil
		},
	}
	var out bytes.Buffer
	if err := ListGenderChangeRequests(context.Background(), client, &out, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "g1") || !strings.Contains(out.String(), "female") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestResolveGenderChangeRequest_ApproveAndReject(t *testing.T) {
	var gotReq *moderationv1.ResolveGenderChangeRequestRequest
	client := &fakeModeration{
		resolveGenderChangeRequest: func(_ context.Context, req *moderationv1.ResolveGenderChangeRequestRequest) (*moderationv1.ResolveGenderChangeRequestResponse, error) {
			gotReq = req
			return &moderationv1.ResolveGenderChangeRequestResponse{}, nil
		},
	}
	var out bytes.Buffer
	if err := ResolveGenderChangeRequest(context.Background(), client, &out, "g1", "admin-1", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gotReq.GetApprove() || gotReq.GetRequestId() != "g1" || gotReq.GetResolvedBy() != "admin-1" {
		t.Fatalf("unexpected request: %+v", gotReq)
	}
	if !strings.Contains(out.String(), "approved") {
		t.Fatalf("unexpected output: %s", out.String())
	}

	out.Reset()
	if err := ResolveGenderChangeRequest(context.Background(), client, &out, "g2", "admin-1", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.GetApprove() {
		t.Fatalf("expected Approve: false, got %+v", gotReq)
	}
	if !strings.Contains(out.String(), "rejected") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}
