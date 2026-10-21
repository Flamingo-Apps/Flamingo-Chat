// Package grpclog provides a gRPC unary interceptor (the gRPC term for
// middleware: a wrapper that runs around every RPC call) that logs one
// structured line per request and records a Prometheus duration
// histogram (DEC-0093) for it - the shared choke point every request
// already passes through, so later observability work (a trace span)
// extends this same interceptor instead of introducing a new one from
// scratch.
package grpclog

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// requestDuration is package-level and registered once via promauto's
// default registerer - safe because each service is its own process
// with its own registry (DEC-0093), so there's no cross-service
// collision the way a shared global would risk in a single binary.
var requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "grpc_server_request_duration_seconds",
	Help:    "Duration of gRPC unary requests in seconds, by method and status code.",
	Buckets: prometheus.DefBuckets,
}, []string{"method", "code"})

// InitializeMetrics creates every unary method's requestDuration series at
// zero (code OK) before the first request arrives. A series that first
// appears already holding its final count gives rate() nothing to measure
// an increase from, so the first burst of traffic after a restart never
// showed up on a dashboard (INC-0016). Call it once every service is
// registered on s, before s.Serve. Streaming methods are skipped:
// UnaryServerInterceptor never observes them.
func InitializeMetrics(s *grpc.Server) {
	for service, info := range s.GetServiceInfo() {
		for _, m := range info.Methods {
			if m.IsClientStream || m.IsServerStream {
				continue
			}
			requestDuration.WithLabelValues("/"+service+"/"+m.Name, codes.OK.String())
		}
	}
}

// UnaryServerInterceptor logs method, duration, and resulting status code
// for every unary RPC, using logger, and records the same duration into
// requestDuration. Structured log fields (not a formatted sentence) are
// the point: the output destination can change later (stdout now, a log
// shipper eventually) without touching call sites.
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
		code := status.Code(err)

		requestDuration.WithLabelValues(info.FullMethod, code.String()).Observe(duration.Seconds())

		attrs := []any{
			"method", info.FullMethod,
			"code", code.String(),
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
