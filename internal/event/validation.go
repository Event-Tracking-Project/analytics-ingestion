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

/*
ErrMissingOwner marks an event that reached the domain without project or org
ownership.

It deliberately does not wrap ErrValidation. These fields come from the
authenticated identity, never from the payload, so an event without them means
the auth middleware or the handler failed to apply one. That is a server fault,
and reporting it as a 400 would blame the client for a bug they cannot fix.
*/
var ErrMissingOwner = errors.New("event has no owner")

// Function to check if an event batch is empty
func (b *Batch) isEmpty() bool {
	return len(b.EventBatch) == 0
}

/*
Validate checks the fields the client is responsible for.

Ownership is not checked here: project_id and org_id are applied by the server
from the API key, so their absence is not a client error. ValidateOwnership
covers them separately.

Validates:
  - EventID
  - Name
  - Timestamp
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

	return nil
}

/*
ValidateOwnership checks the server-derived fields.

Separate from Validate because the failure means something different: a client
cannot cause it, so it maps to a server error rather than a bad request.
*/
func (e Event) ValidateOwnership() error {
	if e.ProjectID == "" {
		return fmt.Errorf("%w: project id is empty", ErrMissingOwner)
	}

	if e.OrgID == "" {
		return fmt.Errorf("%w: organization id is empty", ErrMissingOwner)
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
