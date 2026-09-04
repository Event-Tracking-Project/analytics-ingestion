package storage

import (
	"context"
	"errors"

	"analytics-ingestion/internal/event"
)

// ErrNotFound is returned when an event is not found in the store
var ErrNotFound = errors.New("event not found")

// Store interface defines the methods that a storage implementation must provide for saving, retrieving, listing, and deleting events.
type Store interface {
	Save(ctx context.Context, e event.Event) error
	Get(ctx context.Context, id string) (event.Event, error)
	List(ctx context.Context, projectID string) ([]event.Event, error)
	Delete(ctx context.Context, id string) error
}

var _ Store = (*InMemoryStore)(nil)
