/*
internal/ingest/service.go
Contains the ingestion service: the layer that orchestrates a use case without
knowing anything about HTTP.
Responsibilities:
  - validate incoming events and batches
  - enrich them with server-owned fields (received_at)
  - persist them through the storage.Store interface
  - serve scoped reads and deletes for the CRUD API

A queue will sit between this layer and storage once workers exist.
*/
package ingest

import (
	"context"
	"fmt"
	"time"

	"analytics-ingestion/internal/event"
	"analytics-ingestion/internal/storage"
)

// Service orchestrates event ingestion and retrieval.
// It depends on the storage.Store interface rather than a concrete store, so
// the in-memory implementation can be swapped for a database without changes here.
type Service struct {
	store storage.Store
}

// NewService returns a Service backed by the given store.
func NewService(store storage.Store) *Service {
	return &Service{
		store: store,
	}
}

// Ingest validates, enriches and persists a single event.
// Returns an error wrapping event.ErrValidation when the client's fields are
// bad, or event.ErrMissingOwner when the server failed to attribute it.
func (s *Service) Ingest(ctx context.Context, e event.Event) error {
	if err := e.Validate(); err != nil {
		return err
	}

	// Ownership is checked after validation because it is a different kind of
	// failure: the handler applies it from the API key, so an empty value here
	// is a server bug and must not surface as a 400.
	if err := e.ValidateOwnership(); err != nil {
		return err
	}

	// received_at is server-owned: whatever the client sent is discarded.
	e.ReceivedAt = time.Now().Unix()

	if err := s.store.Save(ctx, e); err != nil {
		return fmt.Errorf("save event %q: %w", e.ID, err)
	}

	return nil
}

// BatchIngest validates, enriches and persists every event in a batch.
//
// Validation is all-or-nothing: an invalid event rejects the whole batch before
// anything is written. A store failure partway through is not rolled back, so
// some events may already be persisted when this returns an error. Resolving
// that properly needs a transactional store or an idempotent retry from the SDK.
func (s *Service) BatchIngest(ctx context.Context, b event.Batch) error {
	if err := b.BatchValidate(); err != nil {
		return err
	}

	for i := range b.EventBatch {
		if err := b.EventBatch[i].ValidateOwnership(); err != nil {
			return fmt.Errorf("event %d from batch %q: %w", i, b.BatchID, err)
		}
	}

	// One timestamp for the whole batch: these events arrived together, so
	// stamping each with its own clock reading would imply precision we lack.
	receivedAt := time.Now().Unix()

	for _, e := range b.EventBatch {
		// e is a per-iteration copy, so this does not mutate the caller's batch.
		e.ReceivedAt = receivedAt

		if err := s.store.Save(ctx, e); err != nil {
			return fmt.Errorf("save event %q from batch %q: %w", e.ID, b.BatchID, err)
		}
	}

	return nil
}

// GetEvent returns one event, scoped to the given project.
// An event belonging to another project is reported as storage.ErrNotFound
// rather than a permission error, so the API does not leak which IDs exist.
func (s *Service) GetEvent(ctx context.Context, projectID, id string) (event.Event, error) {
	e, err := s.store.Get(ctx, id)
	if err != nil {
		return event.Event{}, err
	}

	if e.ProjectID != projectID {
		return event.Event{}, storage.ErrNotFound
	}

	return e, nil
}

// ListEvents returns every stored event belonging to the given project.
func (s *Service) ListEvents(ctx context.Context, projectID string) ([]event.Event, error) {
	return s.store.List(ctx, projectID)
}

// DeleteEvent removes one event, scoped to the given project.
//
// The scope check and the delete are two separate store calls, so a concurrent
// write between them could in principle change what is deleted. The store is
// the only place that can close that gap: a database implementation would use
// DELETE ... WHERE id = $1 AND project_id = $2 as a single statement.
func (s *Service) DeleteEvent(ctx context.Context, projectID, id string) error {
	e, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	if e.ProjectID != projectID {
		return storage.ErrNotFound
	}

	return s.store.Delete(ctx, id)
}
