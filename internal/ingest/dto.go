/*
internal/ingest/dto.go
The wire model for requests and responses.

event.Event is the domain and storage model: it carries fields the client owns
alongside fields the server derives (project_id, org_id) and stamps
(received_at). Decoding a request straight into it made those server-owned
fields settable by anyone, and the only thing preventing that was a line of
handler code.

These request types hold client-owned fields only, so a payload cannot express
ownership at all. The conversion to event.Event is the single place identity is
applied, and it takes identity as an argument rather than reading it from the
payload.
*/
package ingest

import (
	"encoding/json"
	"fmt"
	"strings"

	"analytics-ingestion/internal/auth"
	"analytics-ingestion/internal/event"
)

/*
serverOwnedFields is embedded in a request so that a client sending one of
these is rejected rather than silently ignored.

The fields are json.RawMessage because only their presence matters, not their
value: a RawMessage is nil when the key was absent from the payload, which a
string cannot express (an absent field and an empty one both decode to "").

Both spellings of each name are trapped. The event contract uses project_id and
org_id; this service historically accepted projectid and orgid. Whichever an
SDK sends, it should learn the field is not its to set.
*/
type serverOwnedFields struct {
	ProjectID        json.RawMessage `json:"project_id"`
	ProjectIDCompact json.RawMessage `json:"projectid"`
	OrgID            json.RawMessage `json:"org_id"`
	OrgIDCompact     json.RawMessage `json:"orgid"`
	ReceivedAt       json.RawMessage `json:"received_at"`
}

// present returns the names of any server-owned fields the payload carried.
func (s serverOwnedFields) present() []string {
	candidates := []struct {
		name string
		raw  json.RawMessage
	}{
		{"project_id", s.ProjectID},
		{"projectid", s.ProjectIDCompact},
		{"org_id", s.OrgID},
		{"orgid", s.OrgIDCompact},
		{"received_at", s.ReceivedAt},
	}

	var found []string

	for _, c := range candidates {
		if c.raw != nil {
			found = append(found, c.name)
		}
	}

	return found
}

/*
eventRequest is one event as an SDK sends it.

The identity fields stay pointers so an absent user_id can be told apart from
one explicitly sent as an empty string.
*/
type eventRequest struct {
	ID   string `json:"event_id"`
	Name string `json:"event"`

	Timestamp int64 `json:"timestamp"`

	UserID      *string `json:"user_id"`
	AnonymousID *string `json:"anonymous_id"`
	SessionID   *string `json:"session_id"`

	Properties map[string]any `json:"properties"`
	Context    map[string]any `json:"context"`

	// Embedded, so its fields are promoted and decode from the same object.
	serverOwnedFields
}

/*
validate rejects a payload that tries to set server-owned fields.

This is deliberately narrower than json.DisallowUnknownFields: an unknown field
this service has never heard of is tolerated, so an older server does not
reject a newer SDK that added one. Only the fields the server owns are refused.
*/
func (r eventRequest) validate() error {
	names := r.present()
	if len(names) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%w: %s must not be sent; ownership and receipt time are derived from the API key and the server clock",
		event.ErrValidation, strings.Join(names, ", "),
	)
}

// toEvent builds the domain event, applying the authenticated identity.
// received_at is left zero: the service owns it and stamps it on the way in.
func (r eventRequest) toEvent(identity auth.Identity) event.Event {
	return event.Event{
		ID:        r.ID,
		Name:      r.Name,
		Timestamp: r.Timestamp,

		ProjectID: identity.ProjectID,
		OrgID:     identity.OrgID,

		UserID:      r.UserID,
		AnonymousID: r.AnonymousID,
		SessionID:   r.SessionID,

		Properties: r.Properties,
		Context:    r.Context,
	}
}

// batchRequest is a batch of events as an SDK sends it.
type batchRequest struct {
	BatchID string         `json:"batch_id"`
	Events  []eventRequest `json:"events"`
}

// validate rejects a batch in which any event sets server-owned fields,
// reporting which member failed.
func (b batchRequest) validate() error {
	for i := range b.Events {
		if err := b.Events[i].validate(); err != nil {
			return fmt.Errorf("event %d: %w", i, err)
		}
	}

	return nil
}

// toBatch builds the domain batch, applying one identity to every member so a
// single request cannot write into more than one project.
func (b batchRequest) toBatch(identity auth.Identity) event.Batch {
	events := make([]event.Event, 0, len(b.Events))

	for i := range b.Events {
		events = append(events, b.Events[i].toEvent(identity))
	}

	return event.Batch{
		BatchID:    b.BatchID,
		EventBatch: events,
	}
}

/*
eventsResponse wraps a list of events.

The payload is a JSON object rather than a bare array so it can gain fields
later, a cursor or a total, without breaking a client that already parses it.
An array leaves no room to add anything.
*/
type eventsResponse struct {
	Events []event.Event `json:"events"`
}

/*
newEventsResponse guarantees a non-nil slice.

A nil slice marshals to null, not [], which forces every client to handle two
shapes for "no events". The in-memory store happens to return an empty slice,
but the Store interface makes no such promise, so the guarantee belongs here
where the wire format is decided.
*/
func newEventsResponse(events []event.Event) eventsResponse {
	if events == nil {
		events = []event.Event{}
	}

	return eventsResponse{Events: events}
}
