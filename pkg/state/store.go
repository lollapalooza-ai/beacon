package state

import "context"

// Store defines the interface for workload state persistence.
// The store tracks active workloads so that the orchestrator can recover
// from crashes and ensure orphaned compute is decommissioned.
type Store interface {
	// Initialize sets up the backing store (e.g., runs migrations).
	Initialize(ctx context.Context) error

	// CreateWorkload persists a new workload.
	CreateWorkload(ctx context.Context, workload *Workload) error

	// UpdateWorkload updates an existing workload's state.
	UpdateWorkload(ctx context.Context, workload *Workload) error

	// GetWorkload retrieves a workload by ID.
	GetWorkload(ctx context.Context, id string) (*Workload, error)

	// ListActiveWorkloads returns all workloads that are not in a terminal state.
	// This is used on startup to detect orphaned workloads needing cleanup.
	ListActiveWorkloads(ctx context.Context) ([]*Workload, error)

	// RecordEvent persists a workload lifecycle event for auditability.
	RecordEvent(ctx context.Context, event *WorkloadEvent) error

	// ListEvents retrieves all events for a given workload, ordered by creation time.
	ListEvents(ctx context.Context, workloadID string) ([]*WorkloadEvent, error)

	// Close closes the store and releases resources.
	Close() error
}
