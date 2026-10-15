package ws

import (
	"encoding/json"
	"testing"
)

func TestWithTypeAddsTypeAlongsideFields(t *testing.T) {
	b, err := withType("message_ack", messageAckFrame{ClientMsgID: "c1", MessageID: "m1", SentAtUnix: 100})
	if err != nil {
		t.Fatalf("withType: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got["type"] != "message_ack" || got["client_msg_id"] != "c1" || got["message_id"] != "m1" {
		t.Fatalf("unexpected frame: %+v", got)
	}
}
