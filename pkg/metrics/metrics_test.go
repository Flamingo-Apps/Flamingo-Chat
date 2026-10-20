package metrics

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// freePort asks the OS for an unused TCP port, the same way the real
// services let their own HTTP/gRPC listeners pick one in tests -
// avoids colliding with a fixed port another test (or a real service on
// the dev machine) might already be using.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestServeExposesMetricsEndpoint(t *testing.T) {
	addr := freePort(t)
	go Serve(addr, slog.Default())

	var resp *http.Response
	var err error
	// Serve's own ListenAndServe needs a moment to actually bind -
	// polling rather than a fixed sleep, same pattern the rest of this
	// codebase's own readiness waits use.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/metrics")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	// go_goroutines is one of the default Go-runtime collector's metrics
	// (always present via prometheus.DefaultRegisterer) - its presence
	// is what proves this is actually a real Prometheus exposition, not
	// just any 200 response.
	if !strings.Contains(string(body), "go_goroutines") {
		t.Fatalf("expected the default Go collector's metrics in the output, got:\n%s", body)
	}
}
