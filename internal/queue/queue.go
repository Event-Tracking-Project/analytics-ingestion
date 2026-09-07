package queue

import (
	"analytics-ingestion/internal/event"
	"context"
)

type Queue interface {
	Publish(ctx context.Context, batch event.Batch) error
	Consume(ctx context.Context) (event.Batch, error)
	Ack(ctx context.Context, batchID string) error
}
