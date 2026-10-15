package server

import (
	"context"
	"errors"

	"github.com/Flamingo-Apps/Flamingo-Chat/services/presence/internal/store"
)

type fakeStore struct {
	recorded  []store.Entry
	counts    store.Counts
	recordErr error
	countsErr error
}

func (f *fakeStore) Record(_ context.Context, entries []store.Entry) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	f.recorded = append(f.recorded, entries...)
	return nil
}

func (f *fakeStore) Counts(_ context.Context) (store.Counts, error) {
	if f.countsErr != nil {
		return store.Counts{}, f.countsErr
	}
	return f.counts, nil
}

var errFake = errors.New("fake store error")
