// Queue functions

package queue

import (
	"analytics-ingestion/internal/event"
	"context"
)

// Queue interfce for functions and control
type Queue interface {
	Publish(ctx context.Context, batch event.Batch) error
	Consume(ctx context.Context) (event.Batch, error)
	Ack(ctx context.Context, batchID string) error
}
