// Queue functions

package queue

import (
	"analytics-ingestion/internal/event"
	"context"
	"errors"
)

// Empty queue error
var ErrQueueEmpty = errors.New("queue is empty")

// Message struct for Redis queue
type Message struct {
	ID      string
	BatchID string
	Batch   event.Batch
}

// Queue interfce for functions and control
type Queue interface {
	Publish(ctx context.Context, batch event.Batch) error
	Consume(ctx context.Context, consumer string) (Message, error)
	Ack(ctx context.Context, message Message) error
}
