/*
internal/ingest/service.go
Contains service function for event ingestion
Currently handles one event, batch will be implemented
WIll also contain queue and storage writing functionality for workers.
*/
package ingest

import (
	"context"

	"analytics-ingestion/internal/event"
)

// Service struct
type Service struct{}

// Function to create new service to handle events
func NewService() *Service {
	return &Service{}
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
