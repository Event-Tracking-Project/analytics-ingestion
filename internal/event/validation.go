/*
internal/event/validation.go
Contains singular event validation function
Checks singular event for required JSON data
*/
package event

import "errors"

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
