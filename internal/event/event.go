package event

type Event struct {
	// Timestamp of event
	TimeStamp  int64
	ReceivedAt int64

	// ID of project related to
	ProjectID string
	OrgID     string

	// User id data
	userID      *string
	AnonymousID *string
	SessionID   *string

	// Properties that are different depending on event
	Properties map[string]any
	Context    map[string]any

	// Later once custom events is implemented
	// EventID string
	// Name string
}
