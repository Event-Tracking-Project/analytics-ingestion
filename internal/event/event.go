/*
internal/event/event.go
Holds structs related to event data and batches
Is used throughout analytics-ingestion
Structs:
  - Event: Contains singular event definition
*/
package event

type Event struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"event"`

	// Timestamp of event
	Timestamp  int64 `json:"timestamp"`
	ReceivedAt int64 `json:"received_at,omitempty"`

	// ID of project related to
	ProjectID string `json:"projectid"`
	OrgID     string `json:"orgid"`
	// Add funnel id here later			--TO DO--

	// User id data
	UserID      *string `json:"user_id"`
	AnonymousID *string `json:"anonymous_id"`
	SessionID   *string `json:"session_id"`

	// Properties that are different depending on event
	Properties map[string]any `json:"properties"`
	Context    map[string]any `json:"context"`
}

// Struct containing a batch of events in a list
// Has batch ID for easy identification and logging
type Batch struct {
	BatchID string `json:"batch_id"`

	EventBatch []Event `json:"events"`
}
