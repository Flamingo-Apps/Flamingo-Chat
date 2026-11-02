//go:build defect

// Defect test, no infrastructure needed:
//
//	GOTOOLCHAIN=local go test -tags defect -run TestDefect -v ./internal/presence/...
package presence

import (
	"testing"

	presencev1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/presence/v1"
)

// TestDefect_ClosingOneTabMakesAStillConnectedUserDisappearFromPresence: the
// registry is keyed by account and conn.run calls Remove(accountID) when ANY
// connection of that account closes. A person with the app open in two tabs (or
// a reconnect that overlaps the old socket, which the client does by design)
// vanishes from the online count as soon as one of them closes, even though the
// other is still connected, and nothing ever puts them back: Set is called only
// once per connection, at auth.
func TestDefect_ClosingOneTabMakesAStillConnectedUserDisappearFromPresence(t *testing.T) {
	r := NewRegistry()
	r.Set("alice", presencev1.Category_CATEGORY_GUEST) // tab 1 connects
	r.Set("alice", presencev1.Category_CATEGORY_GUEST) // tab 2 connects
	r.Remove("alice")                                  // tab 2 closes; tab 1 is still open

	if n := len(r.Snapshot()); n != 1 {
		t.Fatalf("alice still has one open connection, but the next heartbeat carries %d entries instead of 1", n)
	}
}
