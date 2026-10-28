package ws

import "sync"

// Connections counts the live WebSocket connections each account has right
// now. The presence registry cannot answer "is this person still here?": it
// keeps one entry per account and drops it when any one connection closes, so
// a second tab or a quick reconnect confuses it. An exact count can, and the
// gateway needs the answer to tell a person who refreshed from one who left.
//
// It counts connections to this process only. With more than one gateway
// replica a person reconnecting to another replica would look gone here, so
// that case needs a shared count first (DEC-0114).
//
// All methods are safe on a nil *Connections, which counts nothing and reports
// every account as present, so a Deps built without one (tests) never cancels.
type Connections struct {
	mu sync.Mutex
	n  map[string]int
}

func NewConnections() *Connections { return &Connections{n: map[string]int{}} }

// Open records a new live connection for accountID.
func (c *Connections) Open(accountID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n[accountID]++
}

// Close records that one connection for accountID has ended and returns how many
// remain.
func (c *Connections) Close(accountID string) int {
	if c == nil {
		return 1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n[accountID]--
	left := c.n[accountID]
	if left <= 0 {
		delete(c.n, accountID)
		return 0
	}
	return left
}

// Active reports whether accountID has at least one live connection.
func (c *Connections) Active(accountID string) bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n[accountID] > 0
}
