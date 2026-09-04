/*
internal/ingest/handler_test.go
Tests for the HTTP layer: decoding, status-code mapping and what the response
body is allowed to say.

These drive the handler through httptest rather than calling writeError alone,
so the assertions cover the wiring a client actually hits.
*/
package ingest

import (
	"context"
	"encoding/json"
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

// readErrStore fails every read and delete with a fixed error.
//
// errStore covers only Save, so the read paths need their own double; embedding
// the interface again means each fake states exactly what it overrides.
type readErrStore struct {
	storage.Store

	err error
}

func (s readErrStore) Get(_ context.Context, _ string) (event.Event, error) {
	return event.Event{}, s.err
}

func (s readErrStore) List(_ context.Context, _ string) ([]event.Event, error) {
	return nil, s.err
}

func (s readErrStore) Delete(_ context.Context, _ string) error {
	return s.err
}

// seed stores events through the service so they carry the identity and the
// server-stamped received_at a real request would have produced.
func seed(t *testing.T, svc *Service, ids ...string) {
	t.Helper()

	for i, id := range ids {
		e := event.Event{
			ID:        id,
			Name:      "checkout_completed",
			Timestamp: int64(1710000000 + i),
			ProjectID: testIdentity.ProjectID,
			OrgID:     testIdentity.OrgID,
		}

		if err := svc.Ingest(t.Context(), e); err != nil {
			t.Fatalf("seeding %q: %v", id, err)
		}
	}
}

// authedRequest builds a request carrying testIdentity and the given path value,
// standing in for the mux and the auth middleware.
func authedRequest(t *testing.T, method, target, id string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(method, target, nil)
	req = req.WithContext(auth.NewContext(req.Context(), testIdentity))

	if id != "" {
		req.SetPathValue("id", id)
	}

	return req
}

func TestGetEventHandler(t *testing.T) {
	store := storage.NewInMemoryStore()
	svc := NewService(store)
	h := NewHandler(svc)

	seed(t, svc, "evt_1")

	t.Run("returns the event", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.GetEvent(rec, authedRequest(t, http.MethodGet, "/v1/events/evt_1", "evt_1"))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body: %q)", rec.Code, http.StatusOK, rec.Body.String())
		}

		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/json")
		}

		var got event.Event
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}

		if got.ID != "evt_1" {
			t.Errorf("event_id = %q, want %q", got.ID, "evt_1")
		}
		if got.ProjectID != testIdentity.ProjectID {
			t.Errorf("project_id = %q, want %q", got.ProjectID, testIdentity.ProjectID)
		}
		if got.ReceivedAt == 0 {
			t.Error("received_at = 0: it should serialize even though the field has no omitempty")
		}
	})

	t.Run("unknown id is 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.GetEvent(rec, authedRequest(t, http.MethodGet, "/v1/events/missing", "missing"))

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

// TestGetEventDoesNotLeakOtherProjects is the tenancy assertion: an event that
// exists but belongs elsewhere must be indistinguishable from one that does not
// exist, or the 403/404 difference becomes an existence oracle.
func TestGetEventDoesNotLeakOtherProjects(t *testing.T) {
	store := storage.NewInMemoryStore()
	svc := NewService(store)
	h := NewHandler(svc)

	// Owned by someone else entirely.
	other := event.Event{
		ID:        "evt_other",
		Name:      "checkout_completed",
		Timestamp: 1710000000,
		ProjectID: "someone_else",
		OrgID:     "another_org",
	}
	if err := svc.Ingest(t.Context(), other); err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	rec := httptest.NewRecorder()
	h.GetEvent(rec, authedRequest(t, http.MethodGet, "/v1/events/evt_other", "evt_other"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d: a cross-project read must not be distinguishable from a miss", rec.Code, http.StatusNotFound)
	}

	if strings.Contains(rec.Body.String(), "someone_else") {
		t.Errorf("body = %q, want no trace of the owning project", rec.Body.String())
	}
}

func TestListEventsHandler(t *testing.T) {
	store := storage.NewInMemoryStore()
	svc := NewService(store)
	h := NewHandler(svc)

	seed(t, svc, "evt_3", "evt_1", "evt_2")

	// One event belonging to a different project, which must not appear.
	if err := svc.Ingest(t.Context(), event.Event{
		ID: "evt_foreign", Name: "c", Timestamp: 1, ProjectID: "someone_else", OrgID: "o",
	}); err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	rec := httptest.NewRecorder()
	h.ListEvents(rec, authedRequest(t, http.MethodGet, "/v1/events", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %q)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got eventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	if len(got.Events) != 3 {
		t.Fatalf("returned %d events, want 3 (the foreign project must be excluded)", len(got.Events))
	}

	// seed stamps ascending timestamps in argument order, so sorting by
	// timestamp must undo the shuffled insertion order.
	wantOrder := []string{"evt_3", "evt_1", "evt_2"}
	for i, want := range wantOrder {
		if got.Events[i].ID != want {
			t.Errorf("event %d = %q, want %q: list is not ordered by timestamp", i, got.Events[i].ID, want)
		}
	}
}

// TestListEventsIsDeterministic guards against the map-iteration randomness the
// in-memory store would otherwise expose: repeated calls must agree.
func TestListEventsIsDeterministic(t *testing.T) {
	store := storage.NewInMemoryStore()
	svc := NewService(store)
	h := NewHandler(svc)

	seed(t, svc, "a", "b", "c", "d", "e", "f", "g", "h")

	var first string

	for i := range 20 {
		rec := httptest.NewRecorder()
		h.ListEvents(rec, authedRequest(t, http.MethodGet, "/v1/events", ""))

		if i == 0 {
			first = rec.Body.String()
			continue
		}

		if rec.Body.String() != first {
			t.Fatalf("call %d returned a different order than call 0", i)
		}
	}
}

func TestListEventsEmptyIsAnArrayNotNull(t *testing.T) {
	h := NewHandler(NewService(storage.NewInMemoryStore()))

	rec := httptest.NewRecorder()
	h.ListEvents(rec, authedRequest(t, http.MethodGet, "/v1/events", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// null would force every client to handle two shapes for "no events".
	if body := strings.TrimSpace(rec.Body.String()); body != `{"events":[]}` {
		t.Errorf("body = %q, want %q", body, `{"events":[]}`)
	}
}

func TestDeleteEventHandler(t *testing.T) {
	store := storage.NewInMemoryStore()
	svc := NewService(store)
	h := NewHandler(svc)

	seed(t, svc, "evt_1")

	rec := httptest.NewRecorder()
	h.DeleteEvent(rec, authedRequest(t, http.MethodDelete, "/v1/events/evt_1", "evt_1"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body: %q)", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty for 204", rec.Body.String())
	}

	// Gone afterwards.
	rec = httptest.NewRecorder()
	h.GetEvent(rec, authedRequest(t, http.MethodGet, "/v1/events/evt_1", "evt_1"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status after delete = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeleteEventCannotReachAnotherProject(t *testing.T) {
	store := storage.NewInMemoryStore()
	svc := NewService(store)
	h := NewHandler(svc)

	if err := svc.Ingest(t.Context(), event.Event{
		ID: "evt_other", Name: "c", Timestamp: 1, ProjectID: "someone_else", OrgID: "o",
	}); err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	rec := httptest.NewRecorder()
	h.DeleteEvent(rec, authedRequest(t, http.MethodDelete, "/v1/events/evt_other", "evt_other"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	// Still there: a cross-project delete must be a no-op, not just a bad status.
	if _, err := store.Get(t.Context(), "evt_other"); err != nil {
		t.Errorf("the event was deleted by a project that does not own it: %v", err)
	}
}

// TestReadPathsMapStoreFailuresTo500 checks the read handlers classify a store
// outage the same way the write handlers already do.
func TestReadPathsMapStoreFailuresTo500(t *testing.T) {
	failing := readErrStore{err: errors.New("disk on fire")}
	h := NewHandler(NewService(failing))

	tests := []struct {
		name   string
		invoke func(w http.ResponseWriter, r *http.Request)
		method string
		id     string
	}{
		{name: "GetEvent", invoke: h.GetEvent, method: http.MethodGet, id: "evt_1"},
		{name: "ListEvents", invoke: h.ListEvents, method: http.MethodGet},
		{name: "DeleteEvent", invoke: h.DeleteEvent, method: http.MethodDelete, id: "evt_1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tt.invoke(rec, authedRequest(t, tt.method, "/v1/events", tt.id))

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}

			if strings.Contains(rec.Body.String(), "disk on fire") {
				t.Errorf("body = %q, want the store error to stay out of the response", rec.Body.String())
			}
		})
	}
}

// TestReadHandlersRequireIdentity extends the wiring-bug guard to the new routes.
func TestReadHandlersRequireIdentity(t *testing.T) {
	h := NewHandler(NewService(storage.NewInMemoryStore()))

	tests := []struct {
		name   string
		invoke func(w http.ResponseWriter, r *http.Request)
	}{
		{name: "GetEvent", invoke: h.GetEvent},
		{name: "ListEvents", invoke: h.ListEvents},
		{name: "DeleteEvent", invoke: h.DeleteEvent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No identity on the context.
			req := httptest.NewRequest(http.MethodGet, "/v1/events/evt_1", nil)
			req.SetPathValue("id", "evt_1")

			rec := httptest.NewRecorder()
			tt.invoke(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}
		})
	}
}
