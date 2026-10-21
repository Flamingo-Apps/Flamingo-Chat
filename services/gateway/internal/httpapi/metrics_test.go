package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// httpRequestCount reads one http_server_request_duration_seconds series
// from the default registry. It gathers rather than calling
// GetMetricWithLabelValues, which would create a missing series and make
// an "is it there" check meaningless. Every test uses its own route so the
// shared, package-level histogram can't leak counts between tests.
func httpRequestCount(t *testing.T, route, code string) (uint64, bool) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != "http_server_request_duration_seconds" {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["route"] == route && labels["code"] == code {
				return m.GetHistogram().GetSampleCount(), true
			}
		}
	}
	return 0, false
}

func TestMetrics_LabelsByRoutePatternNotRawPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /test/pattern/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	h := Metrics(mux)

	for _, id := range []string{"a", "b", "c"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test/pattern/"+id, nil))
	}

	got, ok := httpRequestCount(t, "GET /test/pattern/{id}", "201")
	if !ok || got != 3 {
		t.Fatalf("expected one series for the route pattern with 3 requests, got ok=%v count=%d", ok, got)
	}
}

func TestMetrics_UnmatchedPathsShareOneSeries(t *testing.T) {
	h := Metrics(http.NewServeMux())

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test/nope-1", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test/nope-2", nil))

	got, ok := httpRequestCount(t, "unmatched", "404")
	if !ok || got != 2 {
		t.Fatalf("expected both unknown paths in one unmatched/404 series, got ok=%v count=%d", ok, got)
	}
}

func TestMetrics_HandlerThatOnlyWritesBodyIsRecordedAs200(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /test/implicit-ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})

	Metrics(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test/implicit-ok", nil))

	if got, ok := httpRequestCount(t, "GET /test/implicit-ok", "200"); !ok || got != 1 {
		t.Fatalf("expected 1 request recorded as 200, got ok=%v count=%d", ok, got)
	}
}

// A real server, because hijacking needs a real connection. The handler
// type-asserts http.Hijacker exactly as coder/websocket's Accept does.
func TestMetrics_HijackedConnectionStillWorksAndIsNotTimed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /test/hijack", func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "writer is not a Hijacker", http.StatusInternalServerError)
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 8\r\nConnection: close\r\n\r\nhijacked")
		_ = buf.Flush()
	})
	srv := httptest.NewServer(Metrics(mux))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/test/hijack")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "hijacked" {
		t.Fatalf("expected the hijacked connection's own response, got status %d body %q", resp.StatusCode, body)
	}
	for _, code := range []string{"200", "500"} {
		if _, ok := httpRequestCount(t, "GET /test/hijack", code); ok {
			t.Fatalf("a hijacked connection (a WebSocket session) was timed as a request with code %s", code)
		}
	}
}
