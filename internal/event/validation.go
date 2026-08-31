package event

import "errors"

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
