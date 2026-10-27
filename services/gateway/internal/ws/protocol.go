// Package ws implements the gateway's WebSocket protocol
// (docs/ARCHITECTURE.md §5.2): one JSON object per frame, with a "type"
// field selecting the shape of the rest.
package ws

import "encoding/json"

// envelope is every frame's common shape: enough to dispatch on type
// before decoding the rest into a specific struct.
type envelope struct {
	Type string `json:"type"`
}

// Client-to-server frame bodies.

type authFrame struct {
	Token string `json:"token"`
}

type sendMessageFrame struct {
	RoomID      string `json:"room_id"`
	ClientMsgID string `json:"client_msg_id"`
	Body        string `json:"body"`
}

type matchRequestFrame struct {
	Preference string `json:"preference"`
}

type keepChatFrame struct {
	RoomID string `json:"room_id"`
}

// Server-to-client frame bodies. Each is marshaled alongside its own
// "type" field by withType.

type authOKFrame struct {
	Account accountJSON `json:"account"`
	// Token is only set when the token the client authenticated with was
	// older than session.RefreshAfter (docs/ARCHITECTURE.md §4.3).
	Token string `json:"token,omitempty"`
}

type accountJSON struct {
	ID            string `json:"id"`
	Pseudonym     string `json:"pseudonym"`
	Gender        string `json:"gender"`
	GenderChosen  bool   `json:"gender_chosen"`
	GoogleLinked  bool   `json:"google_linked"`
	CreatedAtUnix int64  `json:"created_at_unix"`
}

type messageFrame struct {
	MessageID       string `json:"message_id"`
	RoomID          string `json:"room_id"`
	SenderAccountID string `json:"sender_account_id"`
	SenderPseudonym string `json:"sender_pseudonym"`
	Body            string `json:"body"`
	SentAtUnix      int64  `json:"sent_at"`
	// ClientMsgID is only ever populated on the sender's own
	// message_ack, sent synchronously as SendMessage's response - the
	// live broadcast of a MessageSent event, received over Redis by
	// every member of the room, doesn't carry it (the Kafka event itself
	// has no such field), so it is left empty there. The sender
	// reconciles their own optimistic UI through message_ack instead.
	ClientMsgID string `json:"client_msg_id,omitempty"`
}

type messageAckFrame struct {
	ClientMsgID string `json:"client_msg_id"`
	MessageID   string `json:"message_id"`
	SentAtUnix  int64  `json:"sent_at"`
}

type matchQueuedFrame struct{}

type matchCancelledFrame struct{}

type matchFoundFrame struct {
	RoomID           string `json:"room_id"`
	PartnerPseudonym string `json:"partner_pseudonym"`
	PartnerAccountID string `json:"partner_account_id"`
}

type matchEndedFrame struct {
	RoomID string `json:"room_id"`
	Reason string `json:"reason"`
}

// keepRequestedFrame tells a member that the OTHER person asked to keep the
// conversation, so the web app can ask whether to save it or carry on
// (DEC-0106). Sent only while the room is not yet saved.
type keepRequestedFrame struct {
	RoomID string `json:"room_id"`
}

type keepStatusFrame struct {
	RoomID string `json:"room_id"`
	// You and Partner are only known with certainty for the connection
	// that just voted (the synchronous VoteKeep response tells it its
	// own vote and whether the room is now kept). The live RoomKept
	// broadcast other members receive over Redis only says the room is
	// now kept, not who voted when - see internal/ws's translate.go.
	You     bool `json:"you"`
	Partner bool `json:"partner"`
	Kept    bool `json:"kept"`
}

// presenceFrame is the shape presence's broadcast.Run already publishes
// to Redis's presence channel verbatim (plain JSON, no envelope-kind tag
// byte - that channel only ever carries this one kind of message) - this
// struct exists so redisLoop can re-wrap it with "type" via withType like
// every other outgoing frame, not to decode a different format.
type presenceFrame struct {
	Total  int64 `json:"total"`
	Guests int64 `json:"guests"`
	Male   int64 `json:"male"`
	Female int64 `json:"female"`
}

type roomJoinedFrame struct {
	RoomID string `json:"room_id"`
}

type roomLeftFrame struct {
	RoomID string `json:"room_id"`
}

// A presence frame (total/guests/male/female counts, broadcast on the
// global "presence" channel) is not implemented here yet - presence
// counts are published straight to Redis by the Presence service itself
// (docs/ARCHITECTURE.md §3.4), not through the tagged proto/events/v1
// envelope convention this package's translate.go handles, and Presence
// doesn't exist yet (T9) to define that format. Added when it does.

type kickedFrame struct {
	Reason string `json:"reason"`
}

type errorFrame struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	ClientMsgID string `json:"client_msg_id,omitempty"`
}

type pongFrame struct{}

// withType marshals body alongside a "type" field, matching every
// server-to-client frame's documented shape (docs/ARCHITECTURE.md §5.2).
func withType(typ string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	m["type"] = json.RawMessage(`"` + typ + `"`)
	return json.Marshal(m)
}
