/*
internal/event/event.go
Holds structs related to event data and batches
Is used throughout analytics-ingestion
Structs:
  - Event: Contains singular event definition
*/
package event

type Event struct {
	ID   string
	Name string

	// Timestamp of event
	Timestamp  int64
	ReceivedAt int64

	// ID of project related to
	ProjectID string
	OrgID     string
	// Add funnel id here later			--TO DO--

	// User id data
	UserID      *string
	AnonymousID *string
	SessionID   *string

	// Properties that are different depending on event
	Properties map[string]any
	Context    map[string]any
}
