// Command admin is tools/admin (DEC-0029, DEC-0050): a command-line
// program, not a long-running service, that calls Moderation (and,
// indirectly through it, Identity) from inside the cluster. Nothing it
// talks to is reachable from outside the cluster, and every action it
// takes records who did it (the -by flag) in Moderation's own
// admin_actions log.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
	moderationv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/moderation/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/tools/admin/internal/cli"
)

const usage = `tools/admin - call Moderation's admin actions from inside the cluster.

Usage:
  admin reports list [-unresolved]
  admin reports resolve -id=<report-id> -by=<admin> -outcome=<text>
  admin ban -account=<id> -reason=<text> -by=<admin>
  admin unban -account=<id> -by=<admin>
  admin gender-requests list [-unresolved]
  admin gender-requests resolve -id=<request-id> -by=<admin> (-approve|-reject)

MODERATION_ADDR (default moderation:50055) selects which Moderation to call.
`

func main() {
	if len(os.Args) < 2 {
		_, _ = fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	conn, err := grpc.NewClient(config.String("MODERATION_ADDR", "moderation:50055"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "dial moderation: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()
	client := moderationv1.NewModerationServiceClient(conn)

	ctx := context.Background()
	if err := dispatch(ctx, client, os.Stdout, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatch(ctx context.Context, client cli.ModerationClient, out io.Writer, args []string) error {
	switch args[0] {
	case "reports":
		return reportsCommand(ctx, client, out, args[1:])
	case "ban":
		fs := flag.NewFlagSet("ban", flag.ContinueOnError)
		account := fs.String("account", "", "account id to ban")
		reason := fs.String("reason", "", "why this account is banned")
		by := fs.String("by", "", "who is taking this action")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *account == "" || *reason == "" || *by == "" {
			return fmt.Errorf("ban requires -account, -reason and -by")
		}
		return cli.BanAccount(ctx, client, out, *account, *reason, *by)
	case "unban":
		fs := flag.NewFlagSet("unban", flag.ContinueOnError)
		account := fs.String("account", "", "account id to unban")
		by := fs.String("by", "", "who is taking this action")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *account == "" || *by == "" {
			return fmt.Errorf("unban requires -account and -by")
		}
		return cli.UnbanAccount(ctx, client, out, *account, *by)
	case "gender-requests":
		return genderRequestsCommand(ctx, client, out, args[1:])
	default:
		_, _ = fmt.Fprint(out, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func reportsCommand(ctx context.Context, client cli.ModerationClient, out io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("reports requires a subcommand: list, resolve")
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("reports list", flag.ContinueOnError)
		unresolved := fs.Bool("unresolved", false, "only show unresolved reports")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return cli.ListReports(ctx, client, out, *unresolved)
	case "resolve":
		fs := flag.NewFlagSet("reports resolve", flag.ContinueOnError)
		id := fs.String("id", "", "report id")
		by := fs.String("by", "", "who is taking this action")
		outcome := fs.String("outcome", "", "what was decided")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" || *by == "" {
			return fmt.Errorf("reports resolve requires -id and -by")
		}
		return cli.ResolveReport(ctx, client, out, *id, *by, *outcome)
	default:
		return fmt.Errorf("unknown reports subcommand %q", args[0])
	}
}

func genderRequestsCommand(ctx context.Context, client cli.ModerationClient, out io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("gender-requests requires a subcommand: list, resolve")
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("gender-requests list", flag.ContinueOnError)
		unresolved := fs.Bool("unresolved", false, "only show unresolved requests")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return cli.ListGenderChangeRequests(ctx, client, out, *unresolved)
	case "resolve":
		fs := flag.NewFlagSet("gender-requests resolve", flag.ContinueOnError)
		id := fs.String("id", "", "request id")
		by := fs.String("by", "", "who is taking this action")
		approve := fs.Bool("approve", false, "approve the request")
		reject := fs.Bool("reject", false, "reject the request")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" || *by == "" {
			return fmt.Errorf("gender-requests resolve requires -id and -by")
		}
		if *approve == *reject {
			return fmt.Errorf("gender-requests resolve requires exactly one of -approve or -reject")
		}
		return cli.ResolveGenderChangeRequest(ctx, client, out, *id, *by, *approve)
	default:
		return fmt.Errorf("unknown gender-requests subcommand %q", args[0])
	}
}
