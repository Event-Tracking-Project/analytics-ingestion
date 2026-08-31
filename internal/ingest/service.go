package ingest

import (
	"context"

	"analytics-ingestion/internal/event"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Ingest(ctx context.Context, e event.Event) error {
	if err := e.Validate(); err != nil {
		return err
	}

	// Add Later:
	// queue.Publish(e)
	// or storage.Write(e)

	return nil
}
