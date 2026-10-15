//go:build e2e

// Package e2e is docs/ARCHITECTURE.md §11's end-to-end smoke test: it
// drives a real, already-running gateway (and, through it, every backend
// service) over HTTP and WebSocket, exactly as a real client would. It
// proves nothing a unit or integration test already proves about one
// service in isolation - it proves the services agree with each other.
//
// Run against a stack already up (`tilt ci` or `tilt up`, which port-
// forwards gateway to :8080 and moderation to :50055 - see the
// Tiltfile):
//
//	go test -tags e2e ./tests/e2e/...
//
// Guest signup is rate-limited to 5 per hour per IP
// (docs/ARCHITECTURE.md §4.4), and this suite's guest accounts total
// exactly that many in a full run, all from the one source IP gateway
// sees through the port-forward (DEC-0087 - one scenario's "reporter" is
// a verified user instead of a second guest specifically to stay at 5,
// not 6). TestMain resets this suite's own rate-limit counters before
// running, so repeated runs during local iteration don't need it done by
// hand.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func gatewayURL() string {
	if v := os.Getenv("GATEWAY_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func gatewayWSURL() string {
	return "ws" + strings.TrimPrefix(gatewayURL(), "http") + "/v1/ws"
}

// httpClient is a thin wrapper over Gateway's HTTP API
// (docs/ARCHITECTURE.md §5.1) - just enough for this smoke test to drive
// the flows it covers, not a general-purpose SDK.
type httpClient struct {
	t     *testing.T
	token string
}

func (c *httpClient) do(method, path string, body any) map[string]any {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, gatewayURL()+path, reader)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		c.t.Fatalf("%s %s: status %d: %s", method, path, resp.StatusCode, b)
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		c.t.Fatalf("%s %s: decode response: %v", method, path, err)
	}
	return m
}

// guest signs up a new guest account, matching POST /v1/auth/guest
// (docs/ARCHITECTURE.md §5.1 - no Turnstile secret is configured in
// local development, so no turnstile_token is needed, DEC-0076's
// sibling for Turnstile itself).
func guest(t *testing.T, pseudonym string) *httpClient {
	t.Helper()
	c := &httpClient{t: t}
	resp := c.do(http.MethodPost, "/v1/auth/guest", map[string]string{"pseudonym": pseudonym})
	token, _ := resp["session_token"].(string)
	if token == "" {
		t.Fatalf("guest signup for %q returned no session_token: %+v", pseudonym, resp)
	}
	c.token = token
	return c
}

// verifiedGoogleUser signs in with the development-only fake Google
// verifier (DEC-0049, services/identity/internal/verify/fake.go) -
// "fake:<sub>:<email>", with email on the college domain so the account
// comes back verified (DEC-0026).
func verifiedGoogleUser(t *testing.T, sub string) *httpClient {
	t.Helper()
	c := &httpClient{t: t}
	idToken := fmt.Sprintf("fake:%s:%s@kiit.ac.in", sub, sub)
	resp := c.do(http.MethodPost, "/v1/auth/google", map[string]string{"id_token": idToken})
	token, _ := resp["session_token"].(string)
	if token == "" {
		t.Fatalf("Google sign-in for %q returned no session_token: %+v", sub, resp)
	}
	c.token = token
	return c
}

// accountID calls GET /v1/me, which - unlike /v1/auth/guest's
// {session_token, account} - returns the account's own fields directly
// at the top level (internal/httpapi/me.go's handleGetMe just writes
// toAccountResponse(...) as the whole body).
func (c *httpClient) accountID(t *testing.T) string {
	t.Helper()
	resp := c.do(http.MethodGet, "/v1/me", nil)
	id, _ := resp["id"].(string)
	if id == "" {
		t.Fatalf("GET /v1/me returned no id: %+v", resp)
	}
	return id
}

// wsClient drives Gateway's WebSocket protocol (docs/ARCHITECTURE.md
// §5.2).
type wsClient struct {
	t    *testing.T
	ctx  context.Context
	conn *websocket.Conn
}

func connectAndAuth(t *testing.T, ctx context.Context, token string) *wsClient {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, gatewayWSURL(), nil)
	if err != nil {
		t.Fatalf("dial %s: %v", gatewayWSURL(), err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })

	w := &wsClient{t: t, ctx: ctx, conn: conn}
	w.send(map[string]any{"type": "auth", "token": token})
	frame := w.recv()
	if frame["type"] != "auth_ok" {
		t.Fatalf("expected auth_ok, got %+v", frame)
	}
	return w
}

func (w *wsClient) send(v any) {
	w.t.Helper()
	if err := wsjson.Write(w.ctx, w.conn, v); err != nil {
		w.t.Fatalf("write: %v", err)
	}
}

func (w *wsClient) recv() map[string]any {
	w.t.Helper()
	var m map[string]any
	if err := wsjson.Read(w.ctx, w.conn, &m); err != nil {
		w.t.Fatalf("read: %v", err)
	}
	return m
}

// recvType reads frames, skipping any whose type isn't want, until it
// sees one or w.ctx's deadline passes. Bounded by wall-clock time, not a
// fixed frame count: a connection only just added to a direct room via
// match_found isn't actually subscribed to that room's channel yet -
// refreshRooms only re-syncs subscriptions periodically (every 25
// seconds, the client's own ping cadence, DEC-0073) - so the first
// message the OTHER side sends there can legitimately take close to
// that long to arrive, with presence broadcasts (every 5 seconds)
// interleaved in the meantime. A frame-count bound would give up long
// before that real, documented delay elapses.
func (w *wsClient) recvType(want string) map[string]any {
	w.t.Helper()
	for {
		frame := w.recv()
		if frame["type"] == want {
			return frame
		}
		if w.ctx.Err() != nil {
			w.t.Fatalf("did not see a %q frame before the context expired", want)
			return nil
		}
	}
}

// testContext's timeout comfortably exceeds refreshRooms's 25-second
// worst case (DEC-0073) plus margin - TestTwoGuestsMatchChatKeepAndReadHistory
// can legitimately need most of that just waiting for the room
// subscription a match_found alone doesn't establish.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	return ctx
}
