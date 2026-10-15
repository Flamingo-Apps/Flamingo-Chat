// Package cli implements tools/admin's actual command logic, separated
// from main.go's flag parsing and gRPC dialing so it can be tested
// against fakes (DEC-0029, DEC-0050: a command-line tool, not a service,
// calling Moderation and Identity from inside the cluster - nothing here
// is reachable from outside it).
package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc"

	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
)

// ModerationClient is the subset of moderationv1.ModerationServiceClient
// this package calls, narrowed so fakes_test.go only has to implement
// what's used.
type ModerationClient interface {
	ListReports(ctx context.Context, req *moderationv1.ListReportsRequest, opts ...grpc.CallOption) (*moderationv1.ListReportsResponse, error)
	ResolveReport(ctx context.Context, req *moderationv1.ResolveReportRequest, opts ...grpc.CallOption) (*moderationv1.ResolveReportResponse, error)
	BanAccount(ctx context.Context, req *moderationv1.BanAccountRequest, opts ...grpc.CallOption) (*moderationv1.BanAccountResponse, error)
	UnbanAccount(ctx context.Context, req *moderationv1.UnbanAccountRequest, opts ...grpc.CallOption) (*moderationv1.UnbanAccountResponse, error)
	ListGenderChangeRequests(ctx context.Context, req *moderationv1.ListGenderChangeRequestsRequest, opts ...grpc.CallOption) (*moderationv1.ListGenderChangeRequestsResponse, error)
	ResolveGenderChangeRequest(ctx context.Context, req *moderationv1.ResolveGenderChangeRequestRequest, opts ...grpc.CallOption) (*moderationv1.ResolveGenderChangeRequestResponse, error)
}

func ListReports(ctx context.Context, client ModerationClient, out io.Writer, onlyUnresolved bool) error {
	resp, err := client.ListReports(ctx, &moderationv1.ListReportsRequest{OnlyUnresolved: onlyUnresolved})
	if err != nil {
		return fmt.Errorf("list reports: %w", err)
	}
	reports := resp.GetReports()
	if len(reports) == 0 {
		_, _ = fmt.Fprintln(out, "no reports")
		return nil
	}
	for _, r := range reports {
		status := "unresolved"
		if r.GetResolved() {
			status = "resolved"
		}
		_, _ = fmt.Fprintf(out, "%s\t%s\treporter=%s\treported=%s\troom=%s\tfiled=%s\t%q\n",
			r.GetId(), status, r.GetReporterAccountId(), r.GetReportedAccountId(), r.GetRoomId(),
			time.Unix(r.GetFiledAtUnix(), 0).UTC().Format(time.RFC3339), r.GetReason())
	}
	return nil
}

func ResolveReport(ctx context.Context, client ModerationClient, out io.Writer, reportID, resolvedBy, outcome string) error {
	if _, err := client.ResolveReport(ctx, &moderationv1.ResolveReportRequest{
		ReportId: reportID, ResolvedBy: resolvedBy, Outcome: outcome,
	}); err != nil {
		return fmt.Errorf("resolve report: %w", err)
	}
	_, _ = fmt.Fprintf(out, "resolved report %s\n", reportID)
	return nil
}

func BanAccount(ctx context.Context, client ModerationClient, out io.Writer, accountID, reason, bannedBy string) error {
	if _, err := client.BanAccount(ctx, &moderationv1.BanAccountRequest{
		AccountId: accountID, Reason: reason, BannedBy: bannedBy,
	}); err != nil {
		return fmt.Errorf("ban account: %w", err)
	}
	_, _ = fmt.Fprintf(out, "banned %s\n", accountID)
	return nil
}

func UnbanAccount(ctx context.Context, client ModerationClient, out io.Writer, accountID, unbannedBy string) error {
	if _, err := client.UnbanAccount(ctx, &moderationv1.UnbanAccountRequest{
		AccountId: accountID, UnbannedBy: unbannedBy,
	}); err != nil {
		return fmt.Errorf("unban account: %w", err)
	}
	_, _ = fmt.Fprintf(out, "unbanned %s\n", accountID)
	return nil
}

func ListGenderChangeRequests(ctx context.Context, client ModerationClient, out io.Writer, onlyUnresolved bool) error {
	resp, err := client.ListGenderChangeRequests(ctx, &moderationv1.ListGenderChangeRequestsRequest{OnlyUnresolved: onlyUnresolved})
	if err != nil {
		return fmt.Errorf("list gender change requests: %w", err)
	}
	requests := resp.GetRequests()
	if len(requests) == 0 {
		_, _ = fmt.Fprintln(out, "no gender change requests")
		return nil
	}
	for _, r := range requests {
		status := "unresolved"
		if r.GetResolved() {
			status = "resolved(approved=" + fmt.Sprint(r.GetApproved()) + ")"
		}
		_, _ = fmt.Fprintf(out, "%s\t%s\taccount=%s\trequested=%s\trequested_at=%s\tnote=%q\n",
			r.GetId(), status, r.GetAccountId(), genderString(r.GetRequestedGender()),
			time.Unix(r.GetRequestedAtUnix(), 0).UTC().Format(time.RFC3339), r.GetNote())
	}
	return nil
}

func ResolveGenderChangeRequest(ctx context.Context, client ModerationClient, out io.Writer, requestID, resolvedBy string, approve bool) error {
	if _, err := client.ResolveGenderChangeRequest(ctx, &moderationv1.ResolveGenderChangeRequestRequest{
		RequestId: requestID, Approve: approve, ResolvedBy: resolvedBy,
	}); err != nil {
		return fmt.Errorf("resolve gender change request: %w", err)
	}
	verb := "rejected"
	if approve {
		verb = "approved"
	}
	_, _ = fmt.Fprintf(out, "%s gender change request %s\n", verb, requestID)
	return nil
}

func genderString(g identityv1.Gender) string {
	switch g {
	case identityv1.Gender_GENDER_MALE:
		return "male"
	case identityv1.Gender_GENDER_FEMALE:
		return "female"
	case identityv1.Gender_GENDER_PREFER_NOT_TO_SAY:
		return "prefer_not_to_say"
	default:
		return "unspecified"
	}
}
