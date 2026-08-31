package ingest

import (
	"encoding/json"
	"net/http"

	"analytics-ingestion/internal/event"

	log "github.com/sirupsen/logrus"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

type IngestEventRequest struct {
	Event       string         `json:"event"`
	Timestamp   int64          `json:"timestamp"`
	ProjectID   string         `json:"projectid"`
	OrgID       string         `json:"orgid"`
	UserID      *string        `json:"user_id"`
	AnonymousID *string        `json:"anonymous_id"`
	SessionID   *string        `json:"session_id"`
	Properties  map[string]any `json:"properties"`
	Context     map[string]any `json:"context"`
}

func (h *Handler) Ingest(w http.ResponseWriter, r *http.Request) {
	var req IngestEventRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	e := event.Event{
		Name:        req.Event,
		Timestamp:   req.Timestamp,
		ProjectID:   req.ProjectID,
		OrgID:       req.OrgID,
		UserID:      req.UserID,
		AnonymousID: req.AnonymousID,
		SessionID:   req.SessionID,
		Properties:  req.Properties,
		Context:     req.Context,
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
