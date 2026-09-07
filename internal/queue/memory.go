package queue

import (
	"context"
	"errors"
	"sync"

	"analytics-ingestion/internal/event"
)

var ErrBatchNotFound = errors.New("batch not found")

type MemoryQueue struct {
	mu       sync.Mutex
	notify   chan struct{}
	pending  []event.Batch
	inflight map[string]event.Batch
}

func NewMemory() *MemoryQueue {
	return &MemoryQueue{
		notify:   make(chan struct{}, 1),
		inflight: make(map[string]event.Batch),
	}
}

func (q *MemoryQueue) Publish(ctx context.Context, batch event.Batch) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	q.mu.Lock()
	q.pending = append(q.pending, batch)
	q.mu.Unlock()

	select {
	case q.notify <- struct{}{}:
	default:
	}

	return nil
}

func (q *MemoryQueue) Consume(ctx context.Context) (event.Batch, error) {
	for {
		q.mu.Lock()
		if len(q.pending) > 0 {
			batch := q.pending[0]
			q.pending = q.pending[1:]
			q.inflight[batch.BatchID] = batch
			q.mu.Unlock()
			return batch, nil
		}
		q.mu.Unlock()

		select {
		case <-ctx.Done():
			return event.Batch{}, ctx.Err()
		case <-q.notify:
		}
	}
}

func (q *MemoryQueue) Ack(ctx context.Context, batchID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if _, ok := q.inflight[batchID]; !ok {
		return ErrBatchNotFound
	}

	delete(q.inflight, batchID)
	return nil
}
