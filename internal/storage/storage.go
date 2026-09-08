// Temporary storage for testing

package storage

import (
	"analytics-ingestion/internal/event"
	"context"
)

type Storage interface {
	StoreEvents(ctx context.Context, events []event.Batch) error
}
