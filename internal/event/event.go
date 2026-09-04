/*
internal/event/event.go
Holds structs related to event data and batches
Is used throughout analytics-ingestion
Structs:
  - Event: Contains singular event definition
*/
package event

/*
Event is the domain and storage model for one event occurrence.

It is not the request model. An incoming payload decodes into
ingest.eventRequest, which carries client-owned fields only; the server-derived
fields below are filled in during that conversion. The json tags here describe
what this service stores and returns.
*/
type Event struct {
	ID   string `json:"event_id"`
	Name string `json:"event"`

	// Timestamp of event.
	// received_at is required by the contract, so it must serialize even when
	// zero: omitempty would make a missing value vanish instead of showing up.
	Timestamp  int64 `json:"timestamp"`
	ReceivedAt int64 `json:"received_at"`

	// Ownership, derived from the API key rather than the payload.
	// These tags govern responses and storage only. Requests decode into
	// ingest.eventRequest, which has no field a client could set them with.
	ProjectID string `json:"project_id"`
	OrgID     string `json:"org_id"`
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
