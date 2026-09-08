package worker

import (
	"analytics-ingestion/internal/event"
	"analytics-ingestion/internal/queue"
	"analytics-ingestion/internal/storage"
	"context"

	log "github.com/sirupsen/logrus"
)

type Worker struct {
	id      string
	queue   queue.Queue
	storage storage.Storage
}

func New(
	id string,
	q queue.Queue,
	s storage.Storage,
) *Worker {
	return &Worker{
		id:      id,
		queue:   q,
		storage: s,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	log.WithField("worker_id", w.id).Debug("Worker started")
	defer log.WithField("worker_id", w.id).Debug("Worker stopped")

	for {
		if err := w.runOnce(ctx); err != nil {
			return err
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) error {
	log.WithField("worker_id", w.id).Debug("Worker started")
	defer log.WithField("worker_id", w.id).Debug("Worker stopped")

	return w.runOnce(ctx)
}

func (w *Worker) runOnce(ctx context.Context) error {
	batch, err := w.queue.Consume(ctx)
	if err != nil {
		return err
	}

	if err := w.process(ctx, batch); err != nil {
		return err
	}

	return nil
}

func (w *Worker) process(ctx context.Context, b event.Batch) error {
	if err := w.storage.StoreEvents(ctx, []event.Batch{b}); err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"worker_id":  w.id,
		"batch_id":   b.BatchID,
		"batch_size": len(b.EventBatch),
	}).Info("Worker processed batch")

	return w.queue.Ack(ctx, b.BatchID)
}
