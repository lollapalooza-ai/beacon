package state

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
)

func setupTestStore(t *testing.T) *SQLiteStore {
	t.Helper()

	dbPath := t.TempDir() + "/test.db"
	store, err := NewSQLiteStore(dbPath, zerolog_nop())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	return store
}

// zerolog_nop returns a no-op zerolog logger for tests.
func zerolog_nop() zerolog.Logger {
	return zerolog.Nop()
}

func TestCreateAndGetWorkload(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	w := &Workload{
		ID:           "test-1",
		Intent:       "Train ResNet",
		State:        StatePending,
		Budget:       50.0,
		Requirements: json.RawMessage(`{"workload_type":"training"}`),
	}

	ctx := context.Background()
	if err := store.CreateWorkload(ctx, w); err != nil {
		t.Fatalf("CreateWorkload failed: %v", err)
	}

	got, err := store.GetWorkload(ctx, "test-1")
	if err != nil {
		t.Fatalf("GetWorkload failed: %v", err)
	}

	if got.ID != "test-1" {
		t.Errorf("expected ID test-1, got %s", got.ID)
	}
	if got.Intent != "Train ResNet" {
		t.Errorf("expected intent 'Train ResNet', got %s", got.Intent)
	}
	if got.State != StatePending {
		t.Errorf("expected state pending, got %s", got.State)
	}
	if got.Budget != 50.0 {
		t.Errorf("expected budget 50.0, got %f", got.Budget)
	}
}

func TestUpdateWorkload(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	w := &Workload{ID: "test-2", Intent: "Test", State: StatePending, Budget: 100.0}
	store.CreateWorkload(ctx, w)

	w.State = StateRunning
	w.InstanceID = "i-12345"
	if err := store.UpdateWorkload(ctx, w); err != nil {
		t.Fatalf("UpdateWorkload failed: %v", err)
	}

	got, _ := store.GetWorkload(ctx, "test-2")
	if got.State != StateRunning {
		t.Errorf("expected state running, got %s", got.State)
	}
	if got.InstanceID != "i-12345" {
		t.Errorf("expected instance i-12345, got %s", got.InstanceID)
	}
}

func TestListActiveWorkloads(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	store.CreateWorkload(ctx, &Workload{ID: "active-1", State: StateRunning, Budget: 10.0})
	store.CreateWorkload(ctx, &Workload{ID: "active-2", State: StateProvisioning, Budget: 20.0})
	store.CreateWorkload(ctx, &Workload{ID: "done-1", State: StateCompleted, Budget: 30.0})
	store.CreateWorkload(ctx, &Workload{ID: "done-2", State: StateFailed, Budget: 40.0})

	active, err := store.ListActiveWorkloads(ctx)
	if err != nil {
		t.Fatalf("ListActiveWorkloads failed: %v", err)
	}

	if len(active) != 2 {
		t.Errorf("expected 2 active workloads, got %d", len(active))
	}
}

func TestRecordAndListEvents(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	store.CreateWorkload(ctx, &Workload{ID: "ev-1", State: StatePending, Budget: 10.0})

	e1 := &WorkloadEvent{WorkloadID: "ev-1", EventType: "state_change", Details: `{"state":"discovering"}`}
	e2 := &WorkloadEvent{WorkloadID: "ev-1", EventType: "discovery", Details: `{"prices_found":5}`}

	store.RecordEvent(ctx, e1)
	store.RecordEvent(ctx, e2)

	events, err := store.ListEvents(ctx, "ev-1")
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
}

func TestGetWorkload_NotFound(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	_, err := store.GetWorkload(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workload")
	}
}

func TestUpdateWorkload_NotFound(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	err := store.UpdateWorkload(context.Background(), &Workload{ID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error updating nonexistent workload")
	}
}
