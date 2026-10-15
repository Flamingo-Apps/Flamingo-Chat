package server

import (
	"context"
	"errors"
	"strconv"

	"google.golang.org/grpc"

	eventsv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/events/v1"
	identityv1 "github.com/Flamingo-Apps/Flamingo-Chat/proto/gen/go/identity/v1"
	"github.com/Flamingo-Apps/Flamingo-Chat/services/moderation/internal/store"
)

var errFake = errors.New("fake error")

// fakeStore is an in-memory stand-in for store.Store.
type fakeStore struct {
	reports              map[string]store.Report
	blocks               map[string][]string
	genderChangeRequests map[string]store.GenderChangeRequest
	adminActions         []store.AdminAction
	banned               map[string]bool

	fileReportErr             error
	blockUserErr              error
	listBlocksErr             error
	requestGenderChangeErr    error
	listReportsErr            error
	resolveReportErr          error
	banAccountErr             error
	unbanAccountErr           error
	listGenderChangeReqErr    error
	resolveGenderChangeReqErr error
	logAdminActionErr         error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		reports: map[string]store.Report{}, blocks: map[string][]string{},
		genderChangeRequests: map[string]store.GenderChangeRequest{}, banned: map[string]bool{},
	}
}

func (f *fakeStore) FileReport(_ context.Context, id, reporterAccountID, reportedAccountID, roomID, reason string) (store.Report, error) {
	if f.fileReportErr != nil {
		return store.Report{}, f.fileReportErr
	}
	r := store.Report{ID: id, ReporterAccountID: reporterAccountID, ReportedAccountID: reportedAccountID, RoomID: roomID, Reason: reason}
	f.reports[id] = r
	return r, nil
}

func (f *fakeStore) BlockUser(_ context.Context, accountID, blockedAccountID string) error {
	if f.blockUserErr != nil {
		return f.blockUserErr
	}
	f.blocks[accountID] = append(f.blocks[accountID], blockedAccountID)
	return nil
}

func (f *fakeStore) ListBlocks(_ context.Context, accountID string) ([]string, error) {
	if f.listBlocksErr != nil {
		return nil, f.listBlocksErr
	}
	return f.blocks[accountID], nil
}

func (f *fakeStore) RequestGenderChange(_ context.Context, id, accountID string, gender identityv1.Gender, note string) (store.GenderChangeRequest, error) {
	if f.requestGenderChangeErr != nil {
		return store.GenderChangeRequest{}, f.requestGenderChangeErr
	}
	g := store.GenderChangeRequest{ID: id, AccountID: accountID, RequestedGender: gender, Note: note}
	f.genderChangeRequests[id] = g
	return g, nil
}

func (f *fakeStore) ListReports(_ context.Context, onlyUnresolved bool) ([]store.Report, error) {
	if f.listReportsErr != nil {
		return nil, f.listReportsErr
	}
	var out []store.Report
	for _, r := range f.reports {
		if onlyUnresolved && r.Resolved {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeStore) ResolveReport(_ context.Context, reportID, resolvedBy, outcome string) error {
	if f.resolveReportErr != nil {
		return f.resolveReportErr
	}
	r, ok := f.reports[reportID]
	if !ok {
		return store.ErrNotFound
	}
	r.Resolved, r.ResolvedBy, r.Outcome = true, resolvedBy, outcome
	f.reports[reportID] = r
	return nil
}

func (f *fakeStore) BanAccount(_ context.Context, accountID, _, _ string) error {
	if f.banAccountErr != nil {
		return f.banAccountErr
	}
	f.banned[accountID] = true
	return nil
}

func (f *fakeStore) UnbanAccount(_ context.Context, accountID, _ string) error {
	if f.unbanAccountErr != nil {
		return f.unbanAccountErr
	}
	if !f.banned[accountID] {
		return store.ErrNotFound
	}
	delete(f.banned, accountID)
	return nil
}

func (f *fakeStore) ListGenderChangeRequests(_ context.Context, onlyUnresolved bool) ([]store.GenderChangeRequest, error) {
	if f.listGenderChangeReqErr != nil {
		return nil, f.listGenderChangeReqErr
	}
	var out []store.GenderChangeRequest
	for _, g := range f.genderChangeRequests {
		if onlyUnresolved && g.Resolved {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (f *fakeStore) ResolveGenderChangeRequest(_ context.Context, requestID string, approve bool, resolvedBy string) error {
	if f.resolveGenderChangeReqErr != nil {
		return f.resolveGenderChangeReqErr
	}
	g, ok := f.genderChangeRequests[requestID]
	if !ok {
		return store.ErrNotFound
	}
	g.Resolved, g.Approved, g.ResolvedBy = true, approve, resolvedBy
	f.genderChangeRequests[requestID] = g
	return nil
}

func (f *fakeStore) LogAdminAction(_ context.Context, _ string, action store.AdminAction) error {
	if f.logAdminActionErr != nil {
		return f.logAdminActionErr
	}
	f.adminActions = append(f.adminActions, action)
	return nil
}

// fakeSets is an in-memory stand-in for sets.Sets.
type fakeSets struct {
	banned      map[string]bool
	mutualAvoid [][2]string
	banErr      error
	unbanErr    error
	addAvoidErr error
}

func newFakeSets() *fakeSets {
	return &fakeSets{banned: map[string]bool{}}
}

func (f *fakeSets) Ban(_ context.Context, accountID string) error {
	if f.banErr != nil {
		return f.banErr
	}
	f.banned[accountID] = true
	return nil
}

func (f *fakeSets) Unban(_ context.Context, accountID string) error {
	if f.unbanErr != nil {
		return f.unbanErr
	}
	delete(f.banned, accountID)
	return nil
}

func (f *fakeSets) AddMutualAvoid(_ context.Context, a, b string) error {
	if f.addAvoidErr != nil {
		return f.addAvoidErr
	}
	f.mutualAvoid = append(f.mutualAvoid, [2]string{a, b})
	return nil
}

// fakeProducer is an in-memory stand-in for events.Producer.
type fakeProducer struct {
	published []*eventsv1.ModerationEvent
	err       error
}

func (f *fakeProducer) Publish(_ context.Context, _ string, event *eventsv1.ModerationEvent) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, event)
	return nil
}

func (f *fakeProducer) Close() error { return nil }

// fakeIdentity is an in-memory stand-in for IdentityClient.
type fakeIdentity struct {
	adminSetGenderErr   error
	adminSetGenderCalls []*identityv1.AdminSetGenderRequest
}

func (f *fakeIdentity) AdminSetGender(_ context.Context, req *identityv1.AdminSetGenderRequest, _ ...grpc.CallOption) (*identityv1.AdminSetGenderResponse, error) {
	f.adminSetGenderCalls = append(f.adminSetGenderCalls, req)
	if f.adminSetGenderErr != nil {
		return nil, f.adminSetGenderErr
	}
	return &identityv1.AdminSetGenderResponse{Account: &identityv1.Account{Id: req.GetAccountId(), Gender: req.GetGender()}}, nil
}

func sequentialIDs(prefix string) IDGenerator {
	n := 0
	return func() string {
		n++
		return prefix + "-" + strconv.Itoa(n)
	}
}
