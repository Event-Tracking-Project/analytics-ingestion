package storage

import (
	"context"
	"sync"

	"analytics-ingestion/internal/event"
)

type MemoryStorage struct {
	mu      sync.RWMutex
	batches []event.Batch
}

func NewMemory() *MemoryStorage {
	return &MemoryStorage{}
}

func (s *MemoryStorage) StoreEvents(ctx context.Context, batches []event.Batch) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.batches = append(s.batches, batches...)
	return nil
}

func (s *MemoryStorage) Batches() []event.Batch {
	s.mu.RLock()
	defer s.mu.RUnlock()

	batches := make([]event.Batch, len(s.batches))
	copy(batches, s.batches)
	return batches
}
