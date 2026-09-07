package worker

import (
	"analytics-ingestion/internal/event"
	"analytics-ingestion/internal/queue"
	"analytics-ingestion/internal/storage"
	"context"
)

type Worker struct {
	queue   queue.Queue
	storage storage.Storage
}

func New(
	q queue.Queue,
	s storage.Storage,
) *Worker {
	return &Worker{
		queue:   q,
		storage: s,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	for {
		batch, err := w.queue.Consume(ctx)
		if err != nil {
			return err
		}

		if err := w.process(ctx, batch); err != nil {
			return err
		}
	}
}

func (w *Worker) process(ctx context.Context, b event.Batch) error {
	if err := w.storage.StoreEvents(ctx, []event.Batch{b}); err != nil {
		return err
	}

	return w.queue.Ack(ctx, b.BatchID)
}
