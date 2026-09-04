/*
internal/storage/memory_test.go
Unit tests for InMemoryStore.
Covers CRUD behaviour, the byProject index invariant, the defensive copying
done by cloneEvent, and concurrent access (meaningful under -race).
*/
package storage

import (
	"context"
	"errors"
	"sync"
	"testing"

	"analytics-ingestion/internal/event"
)

// testEvent builds a valid event for use in tests.
// t.Helper marks this as a helper so a failure inside it is reported at the
// caller's line rather than here.
func testEvent(t *testing.T, id, projectID string) event.Event {
	t.Helper()

	userID := "user_123"

	return event.Event{
		ID:         id,
		Name:       "checkout_completed",
		Timestamp:  1710000000,
		ProjectID:  projectID,
		OrgID:      "org_1",
		UserID:     &userID,
		Properties: map[string]any{"amount": 49.99},
		Context:    map[string]any{"locale": "en-US"},
	}
}

func TestSaveAndGet(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	want := testEvent(t, "evt_1", "proj_1")
	if err := s.Save(ctx, want); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	got, err := s.Get(ctx, "evt_1")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}

	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.ProjectID != want.ProjectID {
		t.Errorf("ProjectID = %q, want %q", got.ProjectID, want.ProjectID)
	}
	if got.Properties["amount"] != 49.99 {
		t.Errorf("Properties[amount] = %v, want 49.99", got.Properties["amount"])
	}
	if got.UserID == nil || *got.UserID != "user_123" {
		t.Errorf("UserID = %v, want pointer to user_123", got.UserID)
	}
}

func TestGetNotFound(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	_, err := s.Get(ctx, "missing")

	// errors.Is unwraps, so this keeps working once errors are wrapped with %w.
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestSaveOverwrites(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	first := testEvent(t, "evt_1", "proj_1")
	if err := s.Save(ctx, first); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	second := testEvent(t, "evt_1", "proj_1")
	second.Name = "checkout_started"
	if err := s.Save(ctx, second); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Get(ctx, "evt_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Documents the current last-write-wins choice. If Save later adopts
	// first-write-wins to match ON CONFLICT DO NOTHING, this test should flip.
	if got.Name != "checkout_started" {
		t.Errorf("Name = %q, want %q (last write should win)", got.Name, "checkout_started")
	}

	events, err := s.List(ctx, "proj_1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 1 {
		t.Errorf("List() returned %d events, want 1 after overwrite", len(events))
	}
}

func TestListFiltersByProject(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	seed := []event.Event{
		testEvent(t, "evt_1", "proj_1"),
		testEvent(t, "evt_2", "proj_1"),
		testEvent(t, "evt_3", "proj_2"),
	}
	for _, e := range seed {
		if err := s.Save(ctx, e); err != nil {
			t.Fatalf("Save(%s) error = %v", e.ID, err)
		}
	}

	tests := []struct {
		name      string
		projectID string
		wantIDs   []string
	}{
		{name: "project with two events", projectID: "proj_1", wantIDs: []string{"evt_1", "evt_2"}},
		{name: "project with one event", projectID: "proj_2", wantIDs: []string{"evt_3"}},
		{name: "unknown project", projectID: "proj_missing", wantIDs: nil},
	}

	for _, tt := range tests {
		// t.Run creates a subtest, so a failure names the specific case.
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.List(ctx, tt.projectID)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}

			if len(got) != len(tt.wantIDs) {
				t.Fatalf("List() returned %d events, want %d", len(got), len(tt.wantIDs))
			}

			// Map iteration order is randomized, so compare as a set.
			gotIDs := make(map[string]bool, len(got))
			for _, e := range got {
				gotIDs[e.ID] = true
			}
			for _, id := range tt.wantIDs {
				if !gotIDs[id] {
					t.Errorf("List() missing event %q, got %v", id, gotIDs)
				}
			}
		})
	}
}

func TestListReturnsEmptyNotNil(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	got, err := s.List(ctx, "proj_missing")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if got == nil {
		t.Error("List() = nil, want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("List() len = %d, want 0", len(got))
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	if err := s.Save(ctx, testEvent(t, "evt_1", "proj_1")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := s.Delete(ctx, "evt_1"); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}

	if _, err := s.Get(ctx, "evt_1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() after Delete error = %v, want ErrNotFound", err)
	}

	// The index must be updated too, not just the events map.
	got, err := s.List(ctx, "proj_1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List() after Delete returned %d events, want 0", len(got))
	}
}

func TestDeleteNotFound(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	if err := s.Delete(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteRemovesEmptyProjectBucket(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	if err := s.Save(ctx, testEvent(t, "evt_1", "proj_1")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := s.Delete(ctx, "evt_1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// White-box check: same package, so the unexported index is reachable.
	// Guards the invariant that empty buckets are not retained.
	if _, ok := s.byProject["proj_1"]; ok {
		t.Error("byProject retained an empty bucket for proj_1")
	}
}

func TestSaveMovingProjectUpdatesIndex(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	if err := s.Save(ctx, testEvent(t, "evt_1", "proj_1")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	moved := testEvent(t, "evt_1", "proj_2")
	if err := s.Save(ctx, moved); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	oldProject, err := s.List(ctx, "proj_1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(oldProject) != 0 {
		t.Errorf("List(proj_1) returned %d events, want 0 after the event moved", len(oldProject))
	}

	newProject, err := s.List(ctx, "proj_2")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(newProject) != 1 {
		t.Errorf("List(proj_2) returned %d events, want 1", len(newProject))
	}
}

func TestGetReturnsIsolatedCopy(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	if err := s.Save(ctx, testEvent(t, "evt_1", "proj_1")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Get(ctx, "evt_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Without cloneEvent these writes would reach into the store's own copy.
	got.Properties["injected"] = true
	got.Context["injected"] = true
	*got.UserID = "attacker"

	fresh, err := s.Get(ctx, "evt_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if _, ok := fresh.Properties["injected"]; ok {
		t.Error("mutating a returned event's Properties changed the stored event")
	}
	if _, ok := fresh.Context["injected"]; ok {
		t.Error("mutating a returned event's Context changed the stored event")
	}
	if *fresh.UserID != "user_123" {
		t.Errorf("UserID = %q, want %q: pointer field was not cloned", *fresh.UserID, "user_123")
	}
}

func TestSaveCopiesInput(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	e := testEvent(t, "evt_1", "proj_1")
	if err := s.Save(ctx, e); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// The caller still holds the original maps; mutating them must not be
	// visible inside the store.
	e.Properties["injected"] = true
	*e.UserID = "attacker"

	got, err := s.Get(ctx, "evt_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if _, ok := got.Properties["injected"]; ok {
		t.Error("mutating the caller's Properties after Save changed the stored event")
	}
	if *got.UserID != "user_123" {
		t.Errorf("UserID = %q, want %q: input pointer was not cloned", *got.UserID, "user_123")
	}
}

func TestListReturnsIsolatedCopies(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	if err := s.Save(ctx, testEvent(t, "evt_1", "proj_1")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.List(ctx, "proj_1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List() returned %d events, want 1", len(got))
	}

	got[0].Properties["injected"] = true

	fresh, err := s.Get(ctx, "evt_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, ok := fresh.Properties["injected"]; ok {
		t.Error("mutating a listed event's Properties changed the stored event")
	}
}

// TestConcurrentAccess hammers the store from many goroutines at once.
// It asserts little on its own; its value is under `go test -race`, which
// detects unsynchronised access to the maps.
func TestConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	const goroutines = 50

	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			id := "evt_" + string(rune('a'+n%26))
			e := testEvent(t, id, "proj_1")

			if err := s.Save(ctx, e); err != nil {
				t.Errorf("Save() error = %v", err)
			}

			// Errors are expected here: another goroutine may delete the same
			// key first. Only unsynchronised access is being tested.
			_, _ = s.Get(ctx, id)
			_, _ = s.List(ctx, "proj_1")
			_ = s.Delete(ctx, id)
		}(i)
	}

	wg.Wait()
}
