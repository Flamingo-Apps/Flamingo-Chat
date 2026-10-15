//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestTwoGuestsMatchChatKeepAndReadHistory covers docs/ARCHITECTURE.md
// §11's first smoke-test flow: two guests matching, chatting, keeping
// the chat, and reading history.
func TestTwoGuestsMatchChatKeepAndReadHistory(t *testing.T) {
	ctx := testContext(t)
	suffix := uuid.NewString()[:8]
	a := guest(t, "smoke-a-"+suffix)
	b := guest(t, "smoke-b-"+suffix)

	wsA := connectAndAuth(t, ctx, a.token)
	wsB := connectAndAuth(t, ctx, b.token)

	wsA.send(map[string]any{"type": "match_request", "preference": "any"})
	wsB.send(map[string]any{"type": "match_request", "preference": "any"})

	foundA := wsA.recvType("match_found")
	foundB := wsB.recvType("match_found")
	roomID, _ := foundA["room_id"].(string)
	if roomID == "" || roomID != foundB["room_id"] {
		t.Fatalf("expected both sides to agree on a room id, got a=%+v b=%+v", foundA, foundB)
	}

	clientMsgID := "e2e-" + suffix
	wsA.send(map[string]any{"type": "send_message", "room_id": roomID, "client_msg_id": clientMsgID, "body": "hello from the smoke test"})
	ack := wsA.recvType("message_ack")
	if ack["client_msg_id"] != clientMsgID {
		t.Fatalf("unexpected message_ack: %+v", ack)
	}
	msgOnB := wsB.recvType("message")
	if msgOnB["body"] != "hello from the smoke test" || msgOnB["room_id"] != roomID {
		t.Fatalf("unexpected message on the other side: %+v", msgOnB)
	}

	wsA.send(map[string]any{"type": "keep_chat", "room_id": roomID})
	wsB.send(map[string]any{"type": "keep_chat", "room_id": roomID})
	keptA := wsA.recvType("keep_status")
	keptB := wsB.recvType("keep_status")
	if keptA["kept"] != true && keptB["kept"] != true {
		t.Fatalf("expected at least one side to see kept: true once both voted, got a=%+v b=%+v", keptA, keptB)
	}

	history := a.do(http.MethodGet, fmt.Sprintf("/v1/rooms/%s/messages", roomID), nil)
	messages, _ := history["messages"].([]any)
	var found bool
	for _, m := range messages {
		entry, _ := m.(map[string]any)
		if entry["body"] == "hello from the smoke test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the sent message in history, got %+v", history)
	}
}

// TestGroupRoomJoinedByInviteCode covers the second flow: a group room
// joined by invite code.
func TestGroupRoomJoinedByInviteCode(t *testing.T) {
	suffix := uuid.NewString()[:8]
	owner := guest(t, "smoke-owner-"+suffix)
	joiner := guest(t, "smoke-joiner-"+suffix)

	created := owner.do(http.MethodPost, "/v1/rooms", map[string]string{"name": "smoke test room " + suffix})
	roomID, _ := created["id"].(string)
	inviteCode, _ := created["invite_code"].(string)
	if roomID == "" || inviteCode == "" {
		t.Fatalf("expected a room id and invite code, got %+v", created)
	}

	joined := joiner.do(http.MethodPost, "/v1/rooms/join", map[string]string{"invite_code": inviteCode})
	if joined["id"] != roomID {
		t.Fatalf("expected the joiner to land in the same room, got %+v", joined)
	}

	rooms := joiner.do(http.MethodGet, "/v1/rooms", nil)
	list, _ := rooms["rooms"].([]any)
	var member bool
	for _, r := range list {
		entry, _ := r.(map[string]any)
		if entry["id"] == roomID {
			member = true
		}
	}
	if !member {
		t.Fatalf("expected the joiner's own room list to include %s, got %+v", roomID, rooms)
	}
}

// TestReportBlockAndBanThroughAdminTool covers the third flow: a
// report, a block, and a ban - the ban specifically through the real
// tools/admin binary, not a direct gRPC call.
func TestReportBlockAndBanThroughAdminTool(t *testing.T) {
	suffix := uuid.NewString()[:8]
	// The reporter is a verified user (the free dev-fake-verifier path,
	// not a guest signup) rather than a second guest - partly realistic
	// (a verified student filing a report), partly practical: this
	// suite's guest accounts alone already total exactly
	// docs/ARCHITECTURE.md §4.4's 5-per-hour-per-IP signup limit (DEC-0087),
	// so this is the one scenario with headroom to not add a sixth.
	reporter := verifiedGoogleUser(t, "smoke-reporter-"+suffix)
	reported := guest(t, "smoke-reported-"+suffix)
	reportedID := reported.accountID(t)

	fileResp := reporter.do(http.MethodPost, "/v1/reports", map[string]string{
		"reported_account_id": reportedID, "reason": "smoke test report " + suffix,
	})
	reportID, _ := fileResp["report_id"].(string)
	if reportID == "" {
		t.Fatalf("expected a report_id, got %+v", fileResp)
	}

	reporter.do(http.MethodPost, "/v1/blocks", map[string]string{"blocked_account_id": reportedID})
	blocks := reporter.do(http.MethodGet, "/v1/blocks", nil)
	blocked, _ := blocks["blocked_account_ids"].([]any)
	var isBlocked bool
	for _, id := range blocked {
		if id == reportedID {
			isBlocked = true
		}
	}
	if !isBlocked {
		t.Fatalf("expected %s in the blocked list, got %+v", reportedID, blocks)
	}

	listOut := runAdmin(t, "reports", "list", "-unresolved")
	if !strings.Contains(listOut, reportID) {
		t.Fatalf("expected admin reports list to show %s, got:\n%s", reportID, listOut)
	}
	runAdmin(t, "reports", "resolve", "-id="+reportID, "-by=e2e-smoke-test", "-outcome=handled by smoke test")

	runAdmin(t, "ban", "-account="+reportedID, "-reason=smoke test ban "+suffix, "-by=e2e-smoke-test")

	// A banned account's existing session should now be rejected
	// (docs/ARCHITECTURE.md §3.5: checked on every authenticated
	// request). delivery-worker's propagation through Kafka/Redis isn't
	// instantaneous, so poll briefly rather than asserting on the very
	// next request.
	deadline := time.Now().Add(10 * time.Second)
	var lastStatus int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, gatewayURL()+"/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+reported.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /v1/me: %v", err)
		}
		lastStatus = resp.StatusCode
		_ = resp.Body.Close()
		if lastStatus == http.StatusForbidden {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastStatus != http.StatusForbidden {
		t.Fatalf("expected the banned account's session to be rejected with 403, last saw %d", lastStatus)
	}

	runAdmin(t, "unban", "-account="+reportedID, "-by=e2e-smoke-test")
}

// TestVerifiedUserThroughDevFakeVerifier covers the fourth flow: a
// verified user through the development-only verifier.
func TestVerifiedUserThroughDevFakeVerifier(t *testing.T) {
	sub := "smoke-verified-" + uuid.NewString()[:8]
	user := verifiedGoogleUser(t, sub)

	// GET /v1/me returns the account's own fields directly, not wrapped
	// in an "account" key the way /v1/auth/guest's response is.
	me := user.do(http.MethodGet, "/v1/me", nil)
	if me["google_linked"] != true {
		t.Fatalf("expected a verified account (google_linked: true), got %+v", me)
	}
	if me["gender_chosen"] == true {
		t.Fatalf("expected a freshly created account to not have chosen a gender yet, got %+v", me)
	}

	set := user.do(http.MethodPost, "/v1/me/gender", map[string]string{"gender": "male"})
	if set["gender"] != "male" || set["gender_chosen"] != true {
		t.Fatalf("expected gender to be set to male, got %+v", set)
	}

	// Gender is one-time (DEC-0026) - a second attempt must be rejected,
	// not silently overwrite it.
	req, _ := http.NewRequest(http.MethodPost, gatewayURL()+"/v1/me/gender", strings.NewReader(`{"gender":"female"}`))
	req.Header.Set("Authorization", "Bearer "+user.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/me/gender (second attempt): %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("expected a second gender-set attempt to be rejected, got status %d", resp.StatusCode)
	}
}
