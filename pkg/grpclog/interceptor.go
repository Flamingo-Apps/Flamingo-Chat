// Package grpclog provides a gRPC unary interceptor (the gRPC term for
// middleware: a wrapper that runs around every RPC call) that logs one
// structured line per request. It's the shared choke point every request
// already passes through, so Phase 3 observability (a trace span, a
// Prometheus duration histogram) extends this same interceptor later
// instead of introducing a new one from scratch.
package grpclog

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor logs method, duration, and resulting status code
// for every unary RPC, using logger. Structured fields (not a formatted
// sentence) are the point: the output destination can change later
// (stdout now, a log shipper in Phase 3) without touching call sites.
func UnaryServerInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		attrs := []any{
			"method", info.FullMethod,
			"code", status.Code(err).String(),
			"duration_ms", duration.Milliseconds(),
		}
		if err != nil {
			attrs = append(attrs, "err", err.Error())
			logger.ErrorContext(ctx, "grpc request", attrs...)
		} else {
			logger.InfoContext(ctx, "grpc request", attrs...)
		}

		return resp, err
	}
}
