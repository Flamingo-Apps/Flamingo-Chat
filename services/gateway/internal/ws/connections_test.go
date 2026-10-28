package ws

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	matchingv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/matching/v1"
)

func TestConnectionsCountsEachAccountExactly(t *testing.T) {
	c := NewConnections()
	c.Open("a")
	c.Open("a") // a second tab
	c.Open("b")
	if !c.Active("a") || !c.Active("b") || c.Active("nobody") {
		t.Fatalf("unexpected activity")
	}
	if left := c.Close("a"); left != 1 {
		t.Fatalf("one tab closing must leave the other: %d", left)
	}
	if !c.Active("a") {
		t.Fatalf("a still has a tab open")
	}
	if left := c.Close("a"); left != 0 || c.Active("a") {
		t.Fatalf("a should be gone: left=%d", left)
	}
	if left := c.Close("a"); left != 0 { // a stray extra close must not go negative or resurrect it
		t.Fatalf("extra close: %d", left)
	}
	c.Open("a")
	if !c.Active("a") {
		t.Fatalf("a reconnecting should be active again")
	}
}

func TestNilConnectionsNeverCancelsAnything(t *testing.T) {
	var c *Connections
	c.Open("a")
	if !c.Active("a") || c.Close("a") == 0 {
		t.Fatalf("a nil counter must report everyone present")
	}
}

// A person who closes the tab while searching must not leave a ghost in the
// queue for the next person to be paired with (INC-0030); one who comes straight
// back, or has another tab open, must keep their place.
func TestAbandonedSearchIsCancelledOnlyWhenThePersonIsReallyGone(t *testing.T) {
	const grace = 60 * time.Millisecond
	setup := func() (*conn, *int32) {
		var cancelled int32
		matching := &fakeMatching{cancelMatch: func(_ context.Context, req *matchingv1.CancelMatchRequest) (*matchingv1.CancelMatchResponse, error) {
			if req.GetAccountId() != "acc-1" {
				t.Errorf("cancelled the wrong account: %q", req.GetAccountId())
			}
			atomic.AddInt32(&cancelled, 1)
			return &matchingv1.CancelMatchResponse{Cancelled: true}, nil
		}}
		c := newTestConn(&fakeIdentity{}, &fakeChat{}, matching)
		c.deps.Connections = NewConnections()
		c.deps.SearchAbandonGrace = grace
		return c, &cancelled
	}
	closeConn := func(c *conn) {
		if c.deps.Connections.Close(c.accountID) == 0 {
			c.cancelSearchIfGone()
		}
	}

	t.Run("gone for good: cancelled once the grace has passed", func(t *testing.T) {
		c, cancelled := setup()
		c.deps.Connections.Open("acc-1")
		closeConn(c)
		if atomic.LoadInt32(cancelled) != 0 {
			t.Fatalf("cancelled before the grace period ended")
		}
		time.Sleep(3 * grace)
		if atomic.LoadInt32(cancelled) != 1 {
			t.Fatalf("expected one cancel, got %d", atomic.LoadInt32(cancelled))
		}
	})

	t.Run("a refresh: back within the grace period keeps the search", func(t *testing.T) {
		c, cancelled := setup()
		c.deps.Connections.Open("acc-1")
		closeConn(c)
		time.Sleep(grace / 3)
		c.deps.Connections.Open("acc-1") // the new page connects
		time.Sleep(3 * grace)
		if atomic.LoadInt32(cancelled) != 0 {
			t.Fatalf("a person who came straight back lost their search")
		}
	})

	t.Run("a second tab still open keeps the search", func(t *testing.T) {
		c, cancelled := setup()
		c.deps.Connections.Open("acc-1")
		c.deps.Connections.Open("acc-1")
		closeConn(c) // one of two tabs closes: nothing is scheduled
		time.Sleep(3 * grace)
		if atomic.LoadInt32(cancelled) != 0 {
			t.Fatalf("closing one of two tabs cancelled the search")
		}
	})

	t.Run("switched off with a zero grace", func(t *testing.T) {
		c, cancelled := setup()
		c.deps.SearchAbandonGrace = 0
		c.deps.Connections.Open("acc-1")
		closeConn(c)
		time.Sleep(3 * grace)
		if atomic.LoadInt32(cancelled) != 0 {
			t.Fatalf("a zero grace must never cancel")
		}
	})
}
