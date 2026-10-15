package server

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
)

func newTestServer() (*Server, *fakeStore, *fakeSets, *fakeProducer, *fakeIdentity) {
	st := newFakeStore()
	se := newFakeSets()
	pr := &fakeProducer{}
	id := &fakeIdentity{}
	return New(st, se, pr, id, sequentialIDs("id")), st, se, pr, id
}

func TestFileReport(t *testing.T) {
	s, _, _, pr, _ := newTestServer()
	resp, err := s.FileReport(context.Background(), &moderationv1.FileReportRequest{
		ReporterAccountId: "a", ReportedAccountId: "b", Reason: "spam",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetReportId() == "" {
		t.Fatal("expected a report id")
	}
	if len(pr.published) != 1 || pr.published[0].GetReportFiled() == nil {
		t.Fatalf("expected a ReportFiled event, got %+v", pr.published)
	}
}

func TestFileReport_StoreErrorBecomesInternal(t *testing.T) {
	s, st, _, _, _ := newTestServer()
	st.fileReportErr = errFake
	_, err := s.FileReport(context.Background(), &moderationv1.FileReportRequest{ReporterAccountId: "a", ReportedAccountId: "b", Reason: "spam"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("got %v, want Internal", err)
	}
}

func TestFileReport_RequiresFields(t *testing.T) {
	s, _, _, _, _ := newTestServer()
	cases := []*moderationv1.FileReportRequest{
		{ReportedAccountId: "b", Reason: "spam"},
		{ReporterAccountId: "a", Reason: "spam"},
		{ReporterAccountId: "a", ReportedAccountId: "b"},
	}
	for _, req := range cases {
		if _, err := s.FileReport(context.Background(), req); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("got %v, want InvalidArgument for %+v", err, req)
		}
	}
}

func TestBlockUser_AddsMutualAvoidAndPublishes(t *testing.T) {
	s, st, se, pr, _ := newTestServer()
	resp, err := s.BlockUser(context.Background(), &moderationv1.BlockUserRequest{AccountId: "a", BlockedAccountId: "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.GetBlocked() {
		t.Fatal("expected Blocked: true")
	}
	if len(st.blocks["a"]) != 1 || st.blocks["a"][0] != "b" {
		t.Fatalf("unexpected blocks: %+v", st.blocks)
	}
	if len(se.mutualAvoid) != 1 || se.mutualAvoid[0] != [2]string{"a", "b"} {
		t.Fatalf("unexpected mutual avoid calls: %+v", se.mutualAvoid)
	}
	if len(pr.published) != 1 || pr.published[0].GetUserBlocked() == nil {
		t.Fatalf("expected a UserBlocked event, got %+v", pr.published)
	}
}

func TestBlockUser_CannotBlockSelf(t *testing.T) {
	s, _, _, _, _ := newTestServer()
	_, err := s.BlockUser(context.Background(), &moderationv1.BlockUserRequest{AccountId: "a", BlockedAccountId: "a"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("got %v, want InvalidArgument", err)
	}
}

func TestListBlocks(t *testing.T) {
	s, st, _, _, _ := newTestServer()
	st.blocks["a"] = []string{"b", "c"}
	resp, err := s.ListBlocks(context.Background(), &moderationv1.ListBlocksRequest{AccountId: "a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.GetBlockedAccountIds()) != 2 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestRequestGenderChange_RejectsUnspecified(t *testing.T) {
	s, _, _, _, _ := newTestServer()
	_, err := s.RequestGenderChange(context.Background(), &moderationv1.RequestGenderChangeRequest{AccountId: "a"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("got %v, want InvalidArgument", err)
	}
}

func TestResolveReport_NotFound(t *testing.T) {
	s, _, _, _, _ := newTestServer()
	_, err := s.ResolveReport(context.Background(), &moderationv1.ResolveReportRequest{ReportId: "nope", ResolvedBy: "admin"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("got %v, want NotFound", err)
	}
}

func TestResolveReport_LogsAdminAction(t *testing.T) {
	s, st, _, _, _ := newTestServer()
	filed, err := s.FileReport(context.Background(), &moderationv1.FileReportRequest{ReporterAccountId: "a", ReportedAccountId: "b", Reason: "spam"})
	if err != nil {
		t.Fatalf("FileReport: %v", err)
	}
	if _, err := s.ResolveReport(context.Background(), &moderationv1.ResolveReportRequest{
		ReportId: filed.GetReportId(), ResolvedBy: "admin-1", Outcome: "warned",
	}); err != nil {
		t.Fatalf("ResolveReport: %v", err)
	}
	if len(st.adminActions) != 1 || st.adminActions[0].Action != "resolve_report" || st.adminActions[0].ActorAccountID != "admin-1" {
		t.Fatalf("unexpected admin actions: %+v", st.adminActions)
	}
}

func TestBanAccount_AddsToSetAndPublishes(t *testing.T) {
	s, st, se, pr, _ := newTestServer()
	_, err := s.BanAccount(context.Background(), &moderationv1.BanAccountRequest{AccountId: "a", Reason: "abuse", BannedBy: "admin-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !se.banned["a"] {
		t.Fatal("expected account to be in the banned set")
	}
	if len(st.adminActions) != 1 || st.adminActions[0].Action != "ban_account" {
		t.Fatalf("unexpected admin actions: %+v", st.adminActions)
	}
	if len(pr.published) != 1 || pr.published[0].GetAccountBanned() == nil {
		t.Fatalf("expected an AccountBanned event, got %+v", pr.published)
	}
}

func TestUnbanAccount_NotFound(t *testing.T) {
	s, _, _, _, _ := newTestServer()
	_, err := s.UnbanAccount(context.Background(), &moderationv1.UnbanAccountRequest{AccountId: "never-banned", UnbannedBy: "admin"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("got %v, want NotFound", err)
	}
}

func TestUnbanAccount_RemovesFromSet(t *testing.T) {
	s, _, se, _, _ := newTestServer()
	if _, err := s.BanAccount(context.Background(), &moderationv1.BanAccountRequest{AccountId: "a", Reason: "abuse", BannedBy: "admin"}); err != nil {
		t.Fatalf("BanAccount: %v", err)
	}
	if _, err := s.UnbanAccount(context.Background(), &moderationv1.UnbanAccountRequest{AccountId: "a", UnbannedBy: "admin"}); err != nil {
		t.Fatalf("UnbanAccount: %v", err)
	}
	if se.banned["a"] {
		t.Fatal("expected account to be removed from the banned set")
	}
}

func TestResolveGenderChangeRequest_ApprovingCallsAdminSetGender(t *testing.T) {
	s, _, _, _, id := newTestServer()
	req, err := s.RequestGenderChange(context.Background(), &moderationv1.RequestGenderChangeRequest{
		AccountId: "a", RequestedGender: identityv1.Gender_GENDER_FEMALE,
	})
	if err != nil {
		t.Fatalf("RequestGenderChange: %v", err)
	}
	_, err = s.ResolveGenderChangeRequest(context.Background(), &moderationv1.ResolveGenderChangeRequestRequest{
		RequestId: req.GetRequestId(), Approve: true, ResolvedBy: "admin-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(id.adminSetGenderCalls) != 1 || id.adminSetGenderCalls[0].GetAccountId() != "a" ||
		id.adminSetGenderCalls[0].GetGender() != identityv1.Gender_GENDER_FEMALE {
		t.Fatalf("unexpected AdminSetGender calls: %+v", id.adminSetGenderCalls)
	}
}

func TestResolveGenderChangeRequest_RejectingDoesNotCallAdminSetGender(t *testing.T) {
	s, _, _, _, id := newTestServer()
	req, err := s.RequestGenderChange(context.Background(), &moderationv1.RequestGenderChangeRequest{
		AccountId: "a", RequestedGender: identityv1.Gender_GENDER_FEMALE,
	})
	if err != nil {
		t.Fatalf("RequestGenderChange: %v", err)
	}
	_, err = s.ResolveGenderChangeRequest(context.Background(), &moderationv1.ResolveGenderChangeRequestRequest{
		RequestId: req.GetRequestId(), Approve: false, ResolvedBy: "admin-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(id.adminSetGenderCalls) != 0 {
		t.Fatalf("expected no AdminSetGender calls, got %+v", id.adminSetGenderCalls)
	}
}

func TestResolveGenderChangeRequest_NotFound(t *testing.T) {
	s, _, _, _, _ := newTestServer()
	_, err := s.ResolveGenderChangeRequest(context.Background(), &moderationv1.ResolveGenderChangeRequestRequest{
		RequestId: "nope", Approve: true, ResolvedBy: "admin",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("got %v, want NotFound", err)
	}
}
