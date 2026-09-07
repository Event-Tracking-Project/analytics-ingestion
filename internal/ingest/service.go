/*
internal/ingest/service.go
Contains service function for event ingestion
Currently handles one event, batch will be implemented
WIll also contain queue and storage writing functionality for workers.
*/
package ingest

import (
	"context"
	"errors"

	"analytics-ingestion/internal/config"
	"analytics-ingestion/internal/event"
)

// Struct for batch ingest validation vars
type BatchResult struct {
	TotalEvents   int
	ValidEvents   int
	InvalidEvents int
}

// Service struct
type Service struct {
	maxBatchSize int
}

// Function to create new service to handle events
func NewService(cfg config.IngestionConfig) *Service {
	return &Service{
		maxBatchSize: cfg.MaxBatchSize,
	}
}

// Ingest function to take event and call validation
// Takes in context and event to produce error if available
func (s *Service) Ingest(ctx context.Context, e event.Event) error {
	if err := e.Validate(); err != nil {
		return err
	}

	// Add Later:
	// queue.Publish(e)
	// or storage.Write(e)

	return nil
}

// Ingest function to take in a batch of events for validation
// Takes in context and event batch to produce error if available
func (s *Service) BatchIngest(ctx context.Context, b event.Batch) (BatchResult, error) {
	total_events := len(b.EventBatch)

	// Enforce max batch size before going with validation
	if s.maxBatchSize > 0 && total_events > s.maxBatchSize {
		return BatchResult{
			TotalEvents: total_events,
		}, errors.New("Batch exceeds maximum event limit")
	}

	validEvents, invalidEvents, err := b.BatchValidate()

	// Batch results post validation
	result := BatchResult{
		TotalEvents:   total_events,
		ValidEvents:   validEvents,
		InvalidEvents: invalidEvents,
	}

	if err != nil {
		return result, err
	}

	// Add Later:
	// queue.Publish(e)
	// or storage.Write(e)

	return result, nil
}
