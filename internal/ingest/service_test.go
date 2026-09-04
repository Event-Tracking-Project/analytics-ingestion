/*
internal/ingest/service_test.go
Unit tests for the ingestion service.
Uses the real in-memory store as a collaborator rather than a mock: it is fast,
already tested, and exercising the real Store interface catches wiring mistakes
a hand-written mock would hide.
*/
package ingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"analytics-ingestion/internal/event"
	"analytics-ingestion/internal/storage"
)

// testEvent builds a valid event for use in tests.
func testEvent(t *testing.T, id, projectID string) event.Event {
	t.Helper()

	return event.Event{
		ID:         id,
		Name:       "checkout_completed",
		Timestamp:  1710000000,
		ProjectID:  projectID,
		OrgID:      "org_1",
		Properties: map[string]any{"amount": 49.99},
	}
}

// errStore is a Store that fails every Save with a fixed error.
//
// Embedding the storage.Store interface means only the method under test has
// to be implemented; the rest satisfy the interface but panic if called, which
// is exactly the feedback wanted from a test double.
type errStore struct {
	storage.Store

	err error
}

func (s errStore) Save(ctx context.Context, e event.Event) error {
	return s.err
}

func TestIngestStoresEvent(t *testing.T) {
	ctx := context.Background()
	store := storage.NewInMemoryStore()
	svc := NewService(store)

	before := time.Now().Unix()

	if err := svc.Ingest(ctx, testEvent(t, "evt_1", "proj_1")); err != nil {
		t.Fatalf("Ingest() error = %v, want nil", err)
	}

	after := time.Now().Unix()

	got, err := store.Get(ctx, "evt_1")
	if err != nil {
		t.Fatalf("Get() error = %v, want the event to have been persisted", err)
	}

	if got.Name != "checkout_completed" {
		t.Errorf("Name = %q, want %q", got.Name, "checkout_completed")
	}

	// ReceivedAt is stamped by the service, so it must land in the window
	// spanning the call.
	if got.ReceivedAt < before || got.ReceivedAt > after {
		t.Errorf("ReceivedAt = %d, want between %d and %d", got.ReceivedAt, before, after)
	}
}

func TestIngestOverwritesClientReceivedAt(t *testing.T) {
	ctx := context.Background()
	store := storage.NewInMemoryStore()
	svc := NewService(store)

	e := testEvent(t, "evt_1", "proj_1")
	e.ReceivedAt = 1 // a client trying to set a server-owned field

	if err := svc.Ingest(ctx, e); err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	got, err := store.Get(ctx, "evt_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.ReceivedAt == 1 {
		t.Error("ReceivedAt = 1: the client-supplied value was not overwritten")
	}
}

func TestIngestRejectsInvalidEvent(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		mutate func(e *event.Event)
	}{
		{name: "missing event id", mutate: func(e *event.Event) { e.ID = "" }},
		{name: "missing name", mutate: func(e *event.Event) { e.Name = "" }},
		{name: "zero timestamp", mutate: func(e *event.Event) { e.Timestamp = 0 }},
		{name: "missing project id", mutate: func(e *event.Event) { e.ProjectID = "" }},
		{name: "missing org id", mutate: func(e *event.Event) { e.OrgID = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := storage.NewInMemoryStore()
			svc := NewService(store)

			e := testEvent(t, "evt_1", "proj_1")
			tt.mutate(&e)

			err := svc.Ingest(ctx, e)
			if !errors.Is(err, event.ErrValidation) {
				t.Fatalf("Ingest() error = %v, want it to wrap ErrValidation", err)
			}

			// A rejected event must not reach the store.
			if _, err := store.Get(ctx, "evt_1"); !errors.Is(err, storage.ErrNotFound) {
				t.Error("invalid event was persisted anyway")
			}
		})
	}
}

func TestIngestPropagatesStoreError(t *testing.T) {
	ctx := context.Background()

	wantErr := errors.New("disk on fire")
	svc := NewService(errStore{err: wantErr})

	err := svc.Ingest(ctx, testEvent(t, "evt_1", "proj_1"))

	// The service wraps with %w, so the cause stays reachable while the message
	// gains context about which event failed.
	if !errors.Is(err, wantErr) {
		t.Fatalf("Ingest() error = %v, want it to wrap %v", err, wantErr)
	}

	// A store failure is not the client's fault, so it must not look like one.
	if errors.Is(err, event.ErrValidation) {
		t.Error("store failure was misreported as a validation error")
	}
}

func TestBatchIngestStoresAllEvents(t *testing.T) {
	ctx := context.Background()
	store := storage.NewInMemoryStore()
	svc := NewService(store)

	b := event.Batch{
		BatchID: "batch_1",
		EventBatch: []event.Event{
			testEvent(t, "evt_1", "proj_1"),
			testEvent(t, "evt_2", "proj_1"),
			testEvent(t, "evt_3", "proj_2"),
		},
	}

	if err := svc.BatchIngest(ctx, b); err != nil {
		t.Fatalf("BatchIngest() error = %v, want nil", err)
	}

	proj1, err := store.List(ctx, "proj_1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(proj1) != 2 {
		t.Errorf("List(proj_1) returned %d events, want 2", len(proj1))
	}

	// Every event in one batch shares a single received_at reading.
	first, err := store.Get(ctx, "evt_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	second, err := store.Get(ctx, "evt_2")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if first.ReceivedAt != second.ReceivedAt {
		t.Errorf("ReceivedAt differs across one batch: %d vs %d", first.ReceivedAt, second.ReceivedAt)
	}
	if first.ReceivedAt == 0 {
		t.Error("ReceivedAt = 0: batch events were not enriched")
	}
}

func TestBatchIngestRejectsInvalidBatch(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		batch event.Batch
	}{
		{
			name:  "missing batch id",
			batch: event.Batch{EventBatch: []event.Event{testEvent(t, "evt_1", "proj_1")}},
		},
		{
			name:  "empty batch",
			batch: event.Batch{BatchID: "batch_1"},
		},
		{
			name: "one invalid event",
			batch: event.Batch{
				BatchID: "batch_1",
				EventBatch: []event.Event{
					testEvent(t, "evt_1", "proj_1"),
					{Name: "no_id_here", Timestamp: 1, ProjectID: "proj_1", OrgID: "org_1"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := storage.NewInMemoryStore()
			svc := NewService(store)

			err := svc.BatchIngest(ctx, tt.batch)
			if !errors.Is(err, event.ErrValidation) {
				t.Fatalf("BatchIngest() error = %v, want it to wrap ErrValidation", err)
			}

			// Validation runs before any write, so a rejected batch stores nothing.
			got, err := store.List(ctx, "proj_1")
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(got) != 0 {
				t.Errorf("rejected batch persisted %d events, want 0", len(got))
			}
		})
	}
}

func TestGetEventScopesToProject(t *testing.T) {
	ctx := context.Background()
	store := storage.NewInMemoryStore()
	svc := NewService(store)

	if err := svc.Ingest(ctx, testEvent(t, "evt_1", "proj_1")); err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	t.Run("owning project can read it", func(t *testing.T) {
		got, err := svc.GetEvent(ctx, "proj_1", "evt_1")
		if err != nil {
			t.Fatalf("GetEvent() error = %v, want nil", err)
		}
		if got.ID != "evt_1" {
			t.Errorf("ID = %q, want %q", got.ID, "evt_1")
		}
	})

	t.Run("other project sees not found", func(t *testing.T) {
		_, err := svc.GetEvent(ctx, "proj_2", "evt_1")
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetEvent() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("unknown id", func(t *testing.T) {
		_, err := svc.GetEvent(ctx, "proj_1", "missing")
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetEvent() error = %v, want ErrNotFound", err)
		}
	})
}

func TestListEvents(t *testing.T) {
	ctx := context.Background()
	store := storage.NewInMemoryStore()
	svc := NewService(store)

	for _, e := range []event.Event{
		testEvent(t, "evt_1", "proj_1"),
		testEvent(t, "evt_2", "proj_2"),
	} {
		if err := svc.Ingest(ctx, e); err != nil {
			t.Fatalf("Ingest() error = %v", err)
		}
	}

	got, err := svc.ListEvents(ctx, "proj_1")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("ListEvents() returned %d events, want 1", len(got))
	}
	if got[0].ID != "evt_1" {
		t.Errorf("ID = %q, want %q", got[0].ID, "evt_1")
	}
}

func TestDeleteEventScopesToProject(t *testing.T) {
	ctx := context.Background()
	store := storage.NewInMemoryStore()
	svc := NewService(store)

	if err := svc.Ingest(ctx, testEvent(t, "evt_1", "proj_1")); err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	// A different project must not be able to delete it.
	if err := svc.DeleteEvent(ctx, "proj_2", "evt_1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("DeleteEvent() error = %v, want ErrNotFound", err)
	}

	if _, err := svc.GetEvent(ctx, "proj_1", "evt_1"); err != nil {
		t.Fatalf("event was deleted by a project that does not own it: %v", err)
	}

	// The owning project can.
	if err := svc.DeleteEvent(ctx, "proj_1", "evt_1"); err != nil {
		t.Fatalf("DeleteEvent() error = %v, want nil", err)
	}

	if _, err := svc.GetEvent(ctx, "proj_1", "evt_1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("GetEvent() after delete error = %v, want ErrNotFound", err)
	}
}
