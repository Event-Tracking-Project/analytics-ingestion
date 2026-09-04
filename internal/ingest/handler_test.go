/*
internal/ingest/handler_test.go
Tests for the HTTP layer: decoding, status-code mapping and what the response
body is allowed to say.

These drive the handler through httptest rather than calling writeError alone,
so the assertions cover the wiring a client actually hits.
*/
package ingest

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"analytics-ingestion/internal/auth"
	"analytics-ingestion/internal/event"
	"analytics-ingestion/internal/storage"

	log "github.com/sirupsen/logrus"
)

/*
TestMain silences logrus for the whole package.

Handlers log on both the success and the 500 paths by design, and that output
interleaves with test results for no benefit. Doing it once here beats a
per-test helper every handler test would have to remember to call.
*/
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)

	os.Exit(m.Run())
}

// testIdentity is what the auth middleware would have resolved from an API key.
var testIdentity = auth.Identity{ProjectID: "proj_1", OrgID: "org_1"}

/*
postJSON sends body to h and returns the recorded response.

The request carries testIdentity on its context, standing in for the auth
middleware. These tests exercise the handler alone; that the middleware
actually populates the context is the auth package's test.
*/
func postJSON(t *testing.T, h http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.NewContext(req.Context(), testIdentity))

	rec := httptest.NewRecorder()
	h(rec, req)

	return rec
}

// validEventJSON is a payload as an SDK should send it: client-owned fields
// only. Ownership arrives with the API key, not in the body.
const validEventJSON = `{
	"event_id": "evt_1",
	"event": "checkout_completed",
	"timestamp": 1710000000,
	"properties": {"amount": 49.99}
}`

func TestIngestHandlerStatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		store      storage.Store
		wantStatus int
		wantBody   string // substring the response must contain
	}{
		{
			name:       "valid event is accepted",
			body:       validEventJSON,
			store:      storage.NewInMemoryStore(),
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "malformed json",
			body:       `{"event_id": `,
			store:      storage.NewInMemoryStore(),
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid JSON",
		},
		{
			name:       "validation failure",
			body:       `{"event": "checkout_completed", "timestamp": 1}`,
			store:      storage.NewInMemoryStore(),
			wantStatus: http.StatusBadRequest,
			wantBody:   "event_id is required",
		},
		{
			name:       "store failure is not the client's fault",
			body:       validEventJSON,
			store:      errStore{err: errors.New("disk on fire")},
			wantStatus: http.StatusInternalServerError,
			wantBody:   "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(NewService(tt.store))
			rec := postJSON(t, h.Ingest, "/v1/event", tt.body)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestIngestHandlerDoesNotLeakInternalErrors(t *testing.T) {
	h := NewHandler(NewService(errStore{err: errors.New("disk on fire")}))
	rec := postJSON(t, h.Ingest, "/v1/event", validEventJSON)

	// The cause belongs in the log, not in a response an untrusted client reads.
	if strings.Contains(rec.Body.String(), "disk on fire") {
		t.Errorf("body = %q, want the underlying store error to stay out of the response", rec.Body.String())
	}
}

func TestBatchIngestHandlerStatusMapping(t *testing.T) {
	batchJSON := `{"batch_id": "batch_1", "events": [` + validEventJSON + `]}`

	tests := []struct {
		name       string
		body       string
		store      storage.Store
		wantStatus int
		wantBody   string
	}{
		{
			name:       "valid batch is accepted",
			body:       batchJSON,
			store:      storage.NewInMemoryStore(),
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "malformed json",
			body:       `{"batch_id":`,
			store:      storage.NewInMemoryStore(),
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid JSON",
		},
		{
			name:       "missing batch id",
			body:       `{"events": [` + validEventJSON + `]}`,
			store:      storage.NewInMemoryStore(),
			wantStatus: http.StatusBadRequest,
			wantBody:   "batch id is required",
		},
		{
			name:       "invalid member reports its index",
			body:       `{"batch_id": "batch_1", "events": [` + validEventJSON + `, {"event": "no_id", "timestamp": 1}]}`,
			store:      storage.NewInMemoryStore(),
			wantStatus: http.StatusBadRequest,
			wantBody:   "event 1:",
		},
		{
			name:       "store failure",
			body:       batchJSON,
			store:      errStore{err: errors.New("disk on fire")},
			wantStatus: http.StatusInternalServerError,
			wantBody:   "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(NewService(tt.store))
			rec := postJSON(t, h.BatchIngest, "/v1/batch", tt.body)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

// TestWriteErrorNotFound covers the 404 branch directly: no route reaches it
// until the read and delete endpoints exist, but the mapping is defined now so
// those handlers inherit it rather than inventing their own.
func TestWriteErrorNotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/event/evt_1", nil)
	rec := httptest.NewRecorder()

	writeError(rec, req, storage.ErrNotFound)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestWriteErrorPreservesWrapping guards the reason validation errors use %w
// rather than %v: a sentinel wrapped several layers deep must still classify,
// and it must classify by identity, not because the message happens to match.
func TestWriteErrorPreservesWrapping(t *testing.T) {
	t.Run("wrapped sentinel classifies", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/batch", nil)
		rec := httptest.NewRecorder()

		// The shape BatchValidate produces: an index wrapped around a wrapped sentinel.
		err := fmt.Errorf("event 3: %w", fmt.Errorf("%w: event_id is required", event.ErrValidation))
		writeError(rec, req, err)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("lookalike message does not classify", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/event", nil)
		rec := httptest.NewRecorder()

		// Same text as a real validation failure, but nothing wraps the sentinel.
		// Matching on message text would wrongly call this a 400.
		writeError(rec, req, errors.New("validation failed: event_id is required"))

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d: classification must use errors.Is, not message text", rec.Code, http.StatusInternalServerError)
		}
	})
}

// spoofedEventJSON claims a project the API key does not own.
const spoofedEventJSON = `{
	"event_id": "evt_spoof",
	"event": "checkout_completed",
	"timestamp": 1710000000,
	"projectid": "victim_project"
}`

func TestIngestRejectsClientSuppliedOwnership(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantName string // the field name the error must call out
	}{
		{
			name:     "legacy project spelling",
			body:     spoofedEventJSON,
			wantName: "projectid",
		},
		{
			name:     "contract project spelling",
			body:     `{"event_id": "e", "event": "c", "timestamp": 1, "project_id": "victim"}`,
			wantName: "project_id",
		},
		{
			name:     "org id",
			body:     `{"event_id": "e", "event": "c", "timestamp": 1, "org_id": "victim"}`,
			wantName: "org_id",
		},
		{
			name:     "received_at is the server's to stamp",
			body:     `{"event_id": "e", "event": "c", "timestamp": 1, "received_at": 99}`,
			wantName: "received_at",
		},
		{
			name:     "null still counts as sent",
			body:     `{"event_id": "e", "event": "c", "timestamp": 1, "project_id": null}`,
			wantName: "project_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := storage.NewInMemoryStore()
			h := NewHandler(NewService(store))

			rec := postJSON(t, h.Ingest, "/v1/event", tt.body)

			// Rejected, not silently ignored: a client that sends a field it does
			// not own is told, rather than getting a 202 and a dropped value.
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}

			if !strings.Contains(rec.Body.String(), tt.wantName) {
				t.Errorf("body = %q, want it to name the field %q", rec.Body.String(), tt.wantName)
			}

			events, err := store.List(t.Context(), testIdentity.ProjectID)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(events) != 0 {
				t.Errorf("a rejected event was stored anyway")
			}
		})
	}
}

// TestIngestUnknownFieldsAreTolerated pins the deliberate asymmetry: fields
// this service owns are refused, fields it has never heard of are ignored, so
// an older server does not reject a newer SDK.
func TestIngestUnknownFieldsAreTolerated(t *testing.T) {
	store := storage.NewInMemoryStore()
	h := NewHandler(NewService(store))

	body := `{"event_id": "evt_1", "event": "c", "timestamp": 1, "some_future_field": {"a": 1}}`

	if rec := postJSON(t, h.Ingest, "/v1/event", body); rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d (body: %q)", rec.Code, http.StatusAccepted, rec.Body.String())
	}
}

func TestIngestStampsOwnershipFromIdentity(t *testing.T) {
	store := storage.NewInMemoryStore()
	h := NewHandler(NewService(store))

	rec := postJSON(t, h.Ingest, "/v1/event", validEventJSON)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body: %q)", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	got, err := store.Get(t.Context(), "evt_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.ProjectID != testIdentity.ProjectID {
		t.Errorf("ProjectID = %q, want %q", got.ProjectID, testIdentity.ProjectID)
	}
	if got.OrgID != testIdentity.OrgID {
		t.Errorf("OrgID = %q, want %q", got.OrgID, testIdentity.OrgID)
	}
	if got.ReceivedAt == 0 {
		t.Error("ReceivedAt = 0: the service did not stamp it")
	}
}

func TestBatchIngestConfinesEveryEventToOneProject(t *testing.T) {
	store := storage.NewInMemoryStore()
	h := NewHandler(NewService(store))

	body := `{"batch_id": "batch_1", "events": [
		{"event_id": "e1", "event": "a", "timestamp": 1},
		{"event_id": "e2", "event": "b", "timestamp": 2},
		{"event_id": "e3", "event": "c", "timestamp": 3}
	]}`

	rec := postJSON(t, h.BatchIngest, "/v1/batch", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body: %q)", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	got, err := store.List(t.Context(), testIdentity.ProjectID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// One identity is applied to every member, so a batch cannot fan out.
	if len(got) != 3 {
		t.Errorf("List(%q) returned %d events, want all 3", testIdentity.ProjectID, len(got))
	}
}

func TestBatchIngestRejectsClientSuppliedOwnershipByIndex(t *testing.T) {
	store := storage.NewInMemoryStore()
	h := NewHandler(NewService(store))

	body := `{"batch_id": "batch_1", "events": [
		{"event_id": "e1", "event": "a", "timestamp": 1},
		{"event_id": "e2", "event": "b", "timestamp": 2, "projectid": "victim"}
	]}`

	rec := postJSON(t, h.BatchIngest, "/v1/batch", body)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	if !strings.Contains(rec.Body.String(), "event 1:") {
		t.Errorf("body = %q, want it to name the offending member", rec.Body.String())
	}

	// Validation precedes any write, so the innocent member is not stored either.
	events, err := store.List(t.Context(), testIdentity.ProjectID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 0 {
		t.Errorf("rejected batch persisted %d events, want 0", len(events))
	}
}

// TestHandlersRequireIdentity covers the wiring bug: a route registered without
// the auth middleware must fail loudly as a server error, never quietly accept
// an event with no owner.
func TestHandlersRequireIdentity(t *testing.T) {
	tests := []struct {
		name    string
		handler func(h *Handler) http.HandlerFunc
		body    string
	}{
		{
			name:    "Ingest",
			handler: func(h *Handler) http.HandlerFunc { return h.Ingest },
			body:    validEventJSON,
		},
		{
			name:    "BatchIngest",
			handler: func(h *Handler) http.HandlerFunc { return h.BatchIngest },
			body:    `{"batch_id": "b1", "events": [` + validEventJSON + `]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := storage.NewInMemoryStore()
			h := NewHandler(NewService(store))

			// Deliberately no identity on the context.
			req := httptest.NewRequest(http.MethodPost, "/v1/event", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			tt.handler(h)(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}

			events, err := store.List(t.Context(), "")
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(events) != 0 {
				t.Errorf("%d unowned events were stored, want 0", len(events))
			}
		})
	}
}
