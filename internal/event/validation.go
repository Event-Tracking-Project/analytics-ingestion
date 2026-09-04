/*
internal/event/validation.go
Contains event and batch validation functions
Checks incoming events for required JSON data
All validation failures wrap ErrValidation so callers can classify them
with errors.Is without matching on message text.
*/
package event

import (
	"errors"
	"fmt"
)

// ErrValidation is the sentinel wrapped by every validation failure in this
// package. Transport layers match it with errors.Is to map a failure to a
// 400 response, rather than assuming every error is the client's fault.
var ErrValidation = errors.New("validation failed")

// Function to check if an event batch is empty
func (b *Batch) isEmpty() bool {
	return len(b.EventBatch) == 0
}

/*
Validate Event
Takes in one event and outputs error if present
Validates:
  - EventID
  - Name
  - Timestamp
  - ProjectID
  - OrgID
*/
func (e Event) Validate() error {
	// The SDK generates event_id, not the server: an ID minted here would be
	// different on every retry, which would defeat the idempotency it exists for.
	if e.ID == "" {
		return fmt.Errorf("%w: event_id is required", ErrValidation)
	}

	if e.Name == "" {
		return fmt.Errorf("%w: event name is required", ErrValidation)
	}

	if e.Timestamp <= 0 {
		return fmt.Errorf("%w: timestamp is required", ErrValidation)
	}

	if e.ProjectID == "" {
		return fmt.Errorf("%w: project id is required", ErrValidation)
	}

	if e.OrgID == "" {
		return fmt.Errorf("%w: organization id is required", ErrValidation)
	}

	return nil
}

// Validate incoming batch and its events
func (b Batch) BatchValidate() error {
	if b.BatchID == "" {
		return fmt.Errorf("%w: batch id is required", ErrValidation)
	}

	if b.isEmpty() {
		return fmt.Errorf("%w: batch contains no events", ErrValidation)
	}

	for i := range b.EventBatch {
		if err := b.EventBatch[i].Validate(); err != nil {
			// Wrapping again keeps ErrValidation reachable through errors.Is
			// while telling the client which event in the batch failed.
			return fmt.Errorf("event %d: %w", i, err)
		}
	}

	return nil
}
