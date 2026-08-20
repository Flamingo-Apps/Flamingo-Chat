package grpclog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

// decodeLastLine parses the last JSON log line written to buf into a map,
// so tests can assert on individual fields without depending on key order.
func decodeLastLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	last := lines[len(lines)-1]
	var fields map[string]any
	if err := json.Unmarshal([]byte(last), &fields); err != nil {
		t.Fatalf("log line was not valid JSON: %v\nline: %s", err, last)
	}
	return fields
}

func TestUnaryServerInterceptor_PassesCallThrough(t *testing.T) {
	var buf bytes.Buffer
	interceptor := UnaryServerInterceptor(newTestLogger(&buf))

	wantResp := "the real response"
	handler := func(ctx context.Context, req any) (any, error) {
		return wantResp, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/identity.v1.IdentityService/CreateAccount"}

	gotResp, err := interceptor(context.Background(), "the real request", info, handler)
	if err != nil {
		t.Fatalf("interceptor returned an error for a successful handler: %v", err)
	}
	if gotResp != wantResp {
		t.Fatalf("interceptor changed the response: got %v, want %v", gotResp, wantResp)
	}
}

func TestUnaryServerInterceptor_PropagatesHandlerError(t *testing.T) {
	var buf bytes.Buffer
	interceptor := UnaryServerInterceptor(newTestLogger(&buf))

	wantErr := status.Error(codes.NotFound, "account not found")
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, wantErr
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/identity.v1.IdentityService/GetAccount"}

	_, err := interceptor(context.Background(), "req", info, handler)
	if !errors.Is(err, wantErr) && status.Code(err) != codes.NotFound {
		t.Fatalf("interceptor changed the error: got %v, want %v", err, wantErr)
	}
}

func TestUnaryServerInterceptor_LogsSuccessFields(t *testing.T) {
	var buf bytes.Buffer
	interceptor := UnaryServerInterceptor(newTestLogger(&buf))

	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/identity.v1.IdentityService/CreateAccount"}

	if _, err := interceptor(context.Background(), "req", info, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fields := decodeLastLine(t, &buf)
	if fields["method"] != "/identity.v1.IdentityService/CreateAccount" {
		t.Errorf("method = %v, want the full method name", fields["method"])
	}
	if fields["code"] != codes.OK.String() {
		t.Errorf("code = %v, want %v", fields["code"], codes.OK.String())
	}
	if _, ok := fields["duration_ms"]; !ok {
		t.Errorf("expected a duration_ms field, got fields: %v", fields)
	}
	if _, hasErr := fields["err"]; hasErr {
		t.Errorf("successful call should not log an err field, got: %v", fields["err"])
	}
}

func TestUnaryServerInterceptor_LogsFailureFields(t *testing.T) {
	var buf bytes.Buffer
	interceptor := UnaryServerInterceptor(newTestLogger(&buf))

	handler := func(ctx context.Context, req any) (any, error) {
		return nil, status.Error(codes.PermissionDenied, "not a college email")
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/identity.v1.IdentityService/VerifyBadge"}

	if _, err := interceptor(context.Background(), "req", info, handler); err == nil {
		t.Fatal("expected an error from the handler")
	}

	fields := decodeLastLine(t, &buf)
	if fields["code"] != codes.PermissionDenied.String() {
		t.Errorf("code = %v, want %v", fields["code"], codes.PermissionDenied.String())
	}
	if fields["err"] == nil {
		t.Errorf("expected an err field for a failed call, got fields: %v", fields)
	}
}

func TestUnaryServerInterceptor_UnknownErrorCodeIsInternal(t *testing.T) {
	var buf bytes.Buffer
	interceptor := UnaryServerInterceptor(newTestLogger(&buf))

	// A handler returning a plain (non-gRPC-status) error should still log
	// a sensible code rather than an empty/zero value - status.Code(err)
	// on a non-status error returns codes.Unknown, not codes.OK.
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, errors.New("something went wrong")
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/identity.v1.IdentityService/GetAccount"}

	if _, err := interceptor(context.Background(), "req", info, handler); err == nil {
		t.Fatal("expected an error from the handler")
	}

	fields := decodeLastLine(t, &buf)
	if fields["code"] != codes.Unknown.String() {
		t.Errorf("code = %v, want %v", fields["code"], codes.Unknown.String())
	}
}
