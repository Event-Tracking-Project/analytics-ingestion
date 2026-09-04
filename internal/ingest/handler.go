/*
internal/ingest/handler.go
Handler functionality is here
This is where it uses a serice to decode event.
Takes http request and decodes json to compare to event struct using a service from service.go

The handler owns the mapping from a service error to an HTTP status: the layers
below it return sentinel-wrapped errors and stay ignorant of HTTP.
*/
package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"analytics-ingestion/internal/auth"
	"analytics-ingestion/internal/event"
	"analytics-ingestion/internal/storage"

	log "github.com/sirupsen/logrus"
)

/*
errNoIdentity means a request reached a handler without an authenticated
identity on its context, which can only happen if the route was registered
without the auth middleware. That is a wiring bug in main, not a client error,
so it maps to 500 and is loud in the log.
*/
var errNoIdentity = errors.New("no identity on request context: route is not wrapped in auth middleware")

// Handler stuct that contains service pointer
type Handler struct {
	service *Service
}

// Function to create new handler for a service producing a Handler pointer
func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

/*
writeError maps a service error onto a status code and response body.

Classification is by errors.Is against the sentinels the lower layers wrap, not
by inspecting message text, so rewording an error cannot silently change a
status code.

Only errors this service defines are echoed to the client. Anything else is a
bug or an infrastructure failure: the detail goes to the log, where operators
can see it, and the client gets a generic message rather than a leaked view of
the store's internals.
*/
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, event.ErrValidation):
		http.Error(w, err.Error(), http.StatusBadRequest)

	case errors.Is(err, storage.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)

	default:
		log.WithError(err).WithFields(log.Fields{
			"method": r.Method,
			"path":   r.URL.Path,
		}).Error("request failed")

		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// Function takes in http request and uses a Handler to execute
// Decodes request and error checks
// Calls service ingest function to validate input
// Writes to header if successful
func (h *Handler) Ingest(w http.ResponseWriter, r *http.Request) {
	// Checked before the body is read: there is no point decoding a request
	// that cannot be attributed to a project.
	identity, ok := auth.IdentityFrom(r.Context())
	if !ok {
		writeError(w, r, errNoIdentity)
		return
	}

	var req eventRequest

	// A body that will not decode is the client's fault, so it is classified
	// here rather than being handed to writeError as an unknown error.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Refuse a payload that tried to set server-owned fields, rather than
	// accepting it and silently dropping the values.
	if err := req.validate(); err != nil {
		writeError(w, r, err)
		return
	}

	// Ownership comes from the API key. eventRequest has no field carrying it,
	// so this conversion is the only way an event acquires an owner.
	e := req.toEvent(identity)

	if err := h.service.Ingest(r.Context(), e); err != nil {
		writeError(w, r, err)
		return
	}

	log.WithFields(log.Fields{
		"event_name":      e.Name,
		"project_id":      e.ProjectID,
		"org_id":          e.OrgID,
		"event_timestamp": e.Timestamp,
		"size_bytes":      r.ContentLength,
	}).Info("Event ingested successfully")

	w.WriteHeader(http.StatusAccepted)
}

// Function takes in http request and uses a Handler to execute
// Decodes request and error checks
// Calls service BatchIngest function to validate input
// Writes to header if successful
func (h *Handler) BatchIngest(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFrom(r.Context())
	if !ok {
		writeError(w, r, errNoIdentity)
		return
	}

	var req batchRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		writeError(w, r, err)
		return
	}

	// Every event in a batch belongs to the key that sent it, so one request
	// cannot write into several projects.
	b := req.toBatch(identity)

	if err := h.service.BatchIngest(r.Context(), b); err != nil {
		writeError(w, r, err)
		return
	}

	log.WithFields(log.Fields{
		"batch_id":    b.BatchID,
		"event_count": len(b.EventBatch),
	}).Info("Event Batch ingested successfully")

	w.WriteHeader(http.StatusAccepted)
}

/*
writeJSON sends a JSON body with the given status.

The payload is marshalled into a buffer before anything is written, rather than
encoding straight to the ResponseWriter. Once WriteHeader has run the status is
on the wire and cannot be taken back, so an encoding failure mid-stream would
leave a truncated body under a 200. Marshalling first means such a failure can
still become a 500.
*/
func writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		writeError(w, r, fmt.Errorf("encode response: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// The client hanging up mid-write is normal and not actionable, so this is
	// logged at debug rather than treated as a server fault.
	if _, err := w.Write(payload); err != nil {
		log.WithError(err).WithFields(log.Fields{
			"method": r.Method,
			"path":   r.URL.Path,
		}).Debug("failed to write response body")
	}
}

// GetEvent serves GET /v1/events/{id}.
// An event belonging to another project is reported as 404 rather than 403, so
// the API does not confirm which event IDs exist.
func (h *Handler) GetEvent(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFrom(r.Context())
	if !ok {
		writeError(w, r, errNoIdentity)
		return
	}

	e, err := h.service.GetEvent(r.Context(), identity.ProjectID, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusOK, e)
}

// ListEvents serves GET /v1/events, scoped to the caller's project.
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFrom(r.Context())
	if !ok {
		writeError(w, r, errNoIdentity)
		return
	}

	// The project is never a query parameter: it comes from the API key, so a
	// caller cannot list another project's events by asking for them.
	events, err := h.service.ListEvents(r.Context(), identity.ProjectID)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusOK, newEventsResponse(events))
}

// DeleteEvent serves DELETE /v1/events/{id}.
func (h *Handler) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFrom(r.Context())
	if !ok {
		writeError(w, r, errNoIdentity)
		return
	}

	if err := h.service.DeleteEvent(r.Context(), identity.ProjectID, r.PathValue("id")); err != nil {
		writeError(w, r, err)
		return
	}

	log.WithFields(log.Fields{
		"event_id":   r.PathValue("id"),
		"project_id": identity.ProjectID,
	}).Info("Event deleted")

	// 204: the delete succeeded and there is nothing meaningful to return.
	w.WriteHeader(http.StatusNoContent)
}
