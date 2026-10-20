// Package metrics exposes Prometheus metrics for a service - the
// client_golang default registry already includes Go runtime and
// process collectors (goroutines, GC, CPU, memory) with no extra code;
// pkg/grpclog's request-duration histogram and anything else a service
// registers via promauto show up here too, since they all share that
// same default registry within one process (DEC-0093... see that
// decision for why this is vendor-neutral rather than tied to any one
// observability backend).
package metrics

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Serve exposes /metrics on addr until the process exits or the server
// fails. Meant to run in its own goroutine (go metrics.Serve(...)) for
// the service's whole lifetime - a failure here is logged, not fatal,
// since losing the metrics endpoint shouldn't take down real traffic on
// the service's actual port.
func Serve(addr string, logger *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	// Same reasoning as gateway's own HTTP server (main.go): a bare
	// ListenAndServe has no read timeout, so a slow or stuck client
	// could tie up a connection indefinitely.
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("metrics server", "err", err)
	}
}
