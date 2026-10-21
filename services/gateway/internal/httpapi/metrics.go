package httpapi

import (
	"bufio"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// requestDuration is gateway's HTTP counterpart to pkg/grpclog's gRPC
// histogram (DEC-0098): gateway serves HTTP, so it had no request metrics
// of its own.
var requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "http_server_request_duration_seconds",
	Help:    "Duration of HTTP requests in seconds, by route pattern and status code. WebSocket sessions are not included.",
	Buckets: prometheus.DefBuckets,
}, []string{"route", "code"})

// Metrics records requestDuration for every request next serves. next must
// be the *http.ServeMux itself, with nothing in between: the mux writes the
// pattern it matched onto the *http.Request it is handed (r.Pattern), and
// that pattern is the route label. Labelling by pattern rather than
// r.URL.Path keeps the number of series fixed - /v1/rooms/{id}/messages is
// one series however many rooms exist, and every unknown path shares one
// "unmatched" series instead of each scanner probe adding a new one.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if sw.hijacked {
			// A WebSocket upgrade: the handler only returns when the
			// session ends, so its duration is a connection lifetime, not
			// a request latency.
			return
		}
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		requestDuration.WithLabelValues(route, strconv.Itoa(sw.status)).Observe(time.Since(start).Seconds())
	})
}

// statusWriter remembers the status code a handler sent. A handler that
// writes a body without calling WriteHeader gets net/http's implicit 200,
// hence the default.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	hijacked    bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Hijack is implemented here, not only reachable through Unwrap, because
// coder/websocket type-asserts http.Hijacker before it tries unwrapping -
// which is also how Metrics learns the request became a WebSocket.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, rw, err
}

// Unwrap lets http.ResponseController reach the real writer for Flush and
// deadlines.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
