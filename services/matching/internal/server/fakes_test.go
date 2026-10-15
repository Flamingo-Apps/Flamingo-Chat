package server

import (
	"context"
	"errors"
	"strconv"

	"google.golang.org/grpc"

	chatv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/chat/v1"
	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/pairing"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/matching/internal/queue"
)

// fakeQueue is an in-memory stand-in for queue.Queue. matchFor maps a
// seeker's account id to the result TryMatch should return for it - set
// up per test, not a real matching algorithm, since that's exactly what
// the real queue package's own integration tests already cover.
type fakeQueue struct {
	matchFor  map[string]queue.MatchResult
	cancelled map[string]bool
	errOn     string
}

func newFakeQueue() *fakeQueue {
	return &fakeQueue{matchFor: map[string]queue.MatchResult{}, cancelled: map[string]bool{}}
}

func (f *fakeQueue) TryMatch(_ context.Context, seeker queue.Candidate) (queue.MatchResult, error) {
	if f.errOn == seeker.AccountID {
		return queue.MatchResult{}, errors.New("fake queue error")
	}
	if result, ok := f.matchFor[seeker.AccountID]; ok {
		return result, nil
	}
	return queue.MatchResult{Matched: false}, nil
}

func (f *fakeQueue) Cancel(_ context.Context, accountID string) (bool, error) {
	return f.cancelled[accountID], nil
}

// fakePairing is an in-memory stand-in for pairing.Cache.
type fakePairing struct {
	byRoom    map[string][2]pairing.Side
	deleted   map[string]bool
	saveErr   error
	getErr    error
	deleteErr error
}

func newFakePairing() *fakePairing {
	return &fakePairing{byRoom: map[string][2]pairing.Side{}, deleted: map[string]bool{}}
}

func (f *fakePairing) Save(_ context.Context, roomID string, a, b pairing.Side) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.byRoom[roomID] = [2]pairing.Side{a, b}
	return nil
}

func (f *fakePairing) Get(_ context.Context, roomID string) (pairing.Side, pairing.Side, bool, error) {
	if f.getErr != nil {
		return pairing.Side{}, pairing.Side{}, false, f.getErr
	}
	sides, ok := f.byRoom[roomID]
	if !ok {
		return pairing.Side{}, pairing.Side{}, false, nil
	}
	return sides[0], sides[1], true, nil
}

func (f *fakePairing) Delete(_ context.Context, roomID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted[roomID] = true
	delete(f.byRoom, roomID)
	return nil
}

// fakeChat is an in-memory stand-in for ChatClient.
type fakeChat struct {
	nextRoomID  string
	createErr   error
	endErr      error
	createCalls []*chatv1.CreateDirectRoomRequest
	endCalls    []*chatv1.EndDirectRoomRequest
}

func (f *fakeChat) CreateDirectRoom(_ context.Context, req *chatv1.CreateDirectRoomRequest, _ ...grpc.CallOption) (*chatv1.CreateDirectRoomResponse, error) {
	f.createCalls = append(f.createCalls, req)
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &chatv1.CreateDirectRoomResponse{Room: &chatv1.Room{Id: f.nextRoomID, Type: chatv1.RoomType_ROOM_TYPE_DIRECT}}, nil
}

func (f *fakeChat) EndDirectRoom(_ context.Context, req *chatv1.EndDirectRoomRequest, _ ...grpc.CallOption) (*chatv1.EndDirectRoomResponse, error) {
	f.endCalls = append(f.endCalls, req)
	if f.endErr != nil {
		return nil, f.endErr
	}
	return &chatv1.EndDirectRoomResponse{}, nil
}

// fakeProducer is an in-memory stand-in for events.Producer.
type fakeProducer struct {
	published []*eventsv1.MatchingEvent
	err       error
}

func (f *fakeProducer) Publish(_ context.Context, _ string, event *eventsv1.MatchingEvent) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, event)
	return nil
}

func (f *fakeProducer) Close() error { return nil }

func sequentialIDs(prefix string) IDGenerator {
	n := 0
	return func() string {
		n++
		return prefix + "-" + strconv.Itoa(n)
	}
}
