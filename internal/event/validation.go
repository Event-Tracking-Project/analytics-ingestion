/*
internal/event/validation.go
Contains singular event validation function
Checks singular event for required JSON data
*/
package event

import (
	"errors"
)

// Function to check if an event batch is empty
func (b *Batch) isEmpty() bool {
	return len(b.EventBatch) == 0
}

/*
Validate Event
Takes in one event and outputs error if present
Validates:
  - Project ID			--TO DO if necessry--
  - Name
  - Timestamp
  - ProjectID
  - OrgID
*/
func (e Event) Validate() error {
	if e.Name == "" {
		return errors.New("Event name is required!")
	}

	if e.Timestamp <= 0 {
		return errors.New("Timestamp is required!")
	}

	if e.ProjectID == "" {
		return errors.New("Project ID required!")
	}

	if e.OrgID == "" {
		return errors.New("Organization ID required!")
	}

	return nil
}

// Validate incoming batch and its events
func (b Batch) BatchValidate() error {
	if b.BatchID == "" {
		return errors.New("Batch Validation Failed: Missing Batch ID")
	}

	if b.isEmpty() {
		return errors.New("Batch Validation Failed: Empty Batch")
	}

	for i := range b.EventBatch {
		if err := b.EventBatch[i].Validate(); err != nil {
			return err
		}
	}

	return nil
}
