/*
internal/worker/worker.go
Main worker functionality
Contains single worker functions
Gives workers instructions for processing and validation

Will change after redis queue implementation
*/
package worker

import (
	"analytics-ingestion/internal/event"
	"analytics-ingestion/internal/queue"
	"analytics-ingestion/internal/storage"
	"context"

	log "github.com/sirupsen/logrus"
)

// Worker struct
type Worker struct {
	id      string
	queue   queue.Queue
	storage storage.Storage
}

// Create new worker with given queue and storage
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

// Run worker, used for running starting a lot
func (w *Worker) Run(ctx context.Context) error {
	log.WithField("worker_id", w.id).Debug("Worker started")
	defer log.WithField("worker_id", w.id).Debug("Worker stopped")

	for {
		if err := w.runOnce(ctx); err != nil {
			return err
		}
	}
}

// Run once, will stop worker once done with proccess
func (w *Worker) RunOnce(ctx context.Context) error {
	log.WithField("worker_id", w.id).Debug("Worker started")
	defer log.WithField("worker_id", w.id).Debug("Worker stopped")

	return w.runOnce(ctx)
}

// Main worker run
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

// Worker processing instructions
// Will take batch to process and validate to then store valid events
func (w *Worker) process(ctx context.Context, b event.Batch) error {
	totalEvents := len(b.EventBatch)
	validEvents, invalidEvents, err := b.BatchValidate()

	log.WithFields(log.Fields{
		"worker_id":      w.id,
		"batch_id":       b.BatchID,
		"total_events":   totalEvents,
		"valid_events":   validEvents,
		"invalid_events": invalidEvents,
	}).Info("Worker validated batch")

	if err != nil {
		return err
	}

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
