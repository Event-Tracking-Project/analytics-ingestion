/*
internal/storage/memory.go
In-memory implementation of the Store interface.
Backed by a map keyed on event ID, guarded by a RWMutex so it is safe for
concurrent use by the HTTP server's goroutine-per-request model.
A secondary index maps project ID -> event IDs so List does not scan every
stored event.
Intended for local development and tests; data is lost on process exit.
*/
package storage

import (
	"context"
	"maps"
	"sync"

	"analytics-ingestion/internal/event"
)

// InMemoryStore is a non-durable Store implementation backed by a map.
// It is safe for concurrent use.
//
// Events are cloned on the way in and on the way out, so callers can never
// reach the store's copy through a shared map or pointer. See cloneEvent.
type InMemoryStore struct {
	// mu guards events and byProject. Read operations take RLock, writes take Lock.
	mu sync.RWMutex

	// events holds stored events keyed by their event ID.
	events map[string]event.Event

	// byProject indexes event IDs by project ID so List can look up a project's
	// events directly instead of filtering the whole events map. The inner map
	// is used as a set: struct{} occupies zero bytes, so only the keys matter.
	//
	// Invariant: byProject holds exactly one entry per stored event, filed under
	// that event's ProjectID, and never keeps an empty inner map.
	byProject map[string]map[string]struct{}
}

// NewInMemoryStore returns a ready-to-use InMemoryStore with its maps allocated.
// The maps must be created here: writing to a nil map panics at runtime.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		events:    make(map[string]event.Event),
		byProject: make(map[string]map[string]struct{}),
	}
}

// Save stores an event, overwriting any existing event with the same ID.
//
// ctx is accepted to satisfy the Store interface; an in-memory write cannot
// block, so there is nothing to cancel. A database implementation would use it.
func (s *InMemoryStore) Save(ctx context.Context, e event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// An overwrite may move the event to a different project. Drop the stale
	// index entry first, or byProject would report it under both.
	if old, ok := s.events[e.ID]; ok && old.ProjectID != e.ProjectID {
		s.unindex(old.ProjectID, old.ID)
	}

	s.events[e.ID] = cloneEvent(e)
	s.index(e.ProjectID, e.ID)

	return nil
}

// Get returns the event with the given ID, or ErrNotFound if no such event exists.
func (s *InMemoryStore) Get(ctx context.Context, id string) (event.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.events[id]
	if !ok {
		return event.Event{}, ErrNotFound
	}

	return cloneEvent(e), nil
}

// List returns all stored events belonging to the given project.
// The order is unspecified and will vary between calls.
func (s *InMemoryStore) List(ctx context.Context, projectID string) ([]event.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// A missing project yields a nil map: len is 0 and ranging over it is a
	// no-op, so an unknown project falls out as an empty result with no branch.
	ids := s.byProject[projectID]

	out := make([]event.Event, 0, len(ids))
	for id := range ids {
		out = append(out, cloneEvent(s.events[id]))
	}

	return out, nil
}

// Delete removes the event with the given ID, or returns ErrNotFound if it does
// not exist.
func (s *InMemoryStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.events[id]
	if !ok {
		return ErrNotFound
	}

	delete(s.events, id)
	s.unindex(e.ProjectID, id)

	return nil
}

// index records an event ID under its project. Callers must hold the write lock.
func (s *InMemoryStore) index(projectID, eventID string) {
	ids, ok := s.byProject[projectID]
	if !ok {
		ids = make(map[string]struct{})
		s.byProject[projectID] = ids
	}

	ids[eventID] = struct{}{}
}

// unindex drops one event ID from a project's bucket, removing the bucket once
// it is empty so byProject does not accumulate empty maps for deleted projects.
// Callers must hold the write lock.
func (s *InMemoryStore) unindex(projectID, eventID string) {
	ids, ok := s.byProject[projectID]
	if !ok {
		return
	}

	delete(ids, eventID)

	if len(ids) == 0 {
		delete(s.byProject, projectID)
	}
}

// cloneEvent returns a copy of e that shares no mutable state with it.
//
// Copying an Event by assignment is not enough: Properties and Context are maps
// and the identity fields are pointers, all of which are reference types, so a
// plain copy would let a caller mutate data held inside the store without the
// lock. Cloning on both Save and Get closes that in both directions.
//
// The clone is shallow with respect to property values: a map or slice nested
// inside a Properties value is still shared. Full isolation would need a
// recursive copy or a JSON round trip, which is not worth the cost here.
func cloneEvent(e event.Event) event.Event {
	// e is already a copy: it was passed by value.
	e.Properties = maps.Clone(e.Properties)
	e.Context = maps.Clone(e.Context)
	e.UserID = clonePtr(e.UserID)
	e.AnonymousID = clonePtr(e.AnonymousID)
	e.SessionID = clonePtr(e.SessionID)

	return e
}

// clonePtr returns a pointer to a copy of *p, or nil if p is nil.
// The type parameter lets one function serve every optional field on Event.
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}

	v := *p

	return &v
}
