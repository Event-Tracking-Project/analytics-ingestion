/*
internal/ingest/handler.go
Handler functionality is here
This is where it uses a serice to decode event.
Takes http request and decodes json to compare to event struct using a service from service.go
*/
package ingest

import (
	"encoding/json"
	"net/http"

	"analytics-ingestion/internal/event"

	log "github.com/sirupsen/logrus"
)

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

// Function takes in http request and uses a Handler to execute
// Decodes request and error checks
// Calls service ingest function to validate input
// Writes to header if successful
func (h *Handler) Ingest(w http.ResponseWriter, r *http.Request) {
	var e event.Event

	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if err := h.service.Ingest(r.Context(), e); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	var b event.Batch

	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if err := h.service.BatchIngest(r.Context(), b); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.WithFields(log.Fields{
		"batch_id":    b.BatchID,
		"event_count": len(b.EventBatch),
	}).Info("Event Batch ingested successfully")

	w.WriteHeader(http.StatusAccepted)
}
