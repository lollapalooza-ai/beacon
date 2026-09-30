// Package state provides persistence for workload lifecycle state.
// It tracks active workloads and their transitions through the state machine,
// enabling crash recovery by resuming teardown of orphaned compute.
package state

import (
	"encoding/json"
	"time"
)

// WorkloadState represents the current phase of a workload in the state machine.
type WorkloadState string

const (
	StatePending         WorkloadState = "pending"
	StateDiscovering     WorkloadState = "discovering"
	StateBidding         WorkloadState = "bidding"
	StateAuthorizing     WorkloadState = "authorizing"
	StateProvisioning    WorkloadState = "provisioning"
	StateRunning         WorkloadState = "running"
	StateDecommissioning WorkloadState = "decommissioning"
	StateCompleted       WorkloadState = "completed"
	StateFailed          WorkloadState = "failed"
)

// IsTerminal returns true if the state is a final state (completed or failed).
func (s WorkloadState) IsTerminal() bool {
	return s == StateCompleted || s == StateFailed
}

// Workload represents a tracked workload and its lifecycle state.
type Workload struct {
	ID               string          `json:"id"`
	Intent           string          `json:"intent"`                          // original user intent string
	State            WorkloadState   `json:"state"`
	Requirements     json.RawMessage `json:"requirements,omitempty"`          // serialized ComputeRequirements
	SelectedOffer    json.RawMessage `json:"selected_offer,omitempty"`        // serialized SpotPrice
	InstanceID       string          `json:"instance_id,omitempty"`           // cloud instance ID
	InstanceProvider string          `json:"instance_provider,omitempty"`     // "aws", "gcp"
	InstanceRegion   string          `json:"instance_region,omitempty"`       // region the instance is in
	PaymentTokenID   string          `json:"payment_token_id,omitempty"`      // payment authorization token
	Budget           float64         `json:"budget"`                          // max budget in USD
	AmountSpent      float64         `json:"amount_spent"`                    // actual spend so far
	Error            string          `json:"error,omitempty"`                 // error details if failed
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// WorkloadEvent records a significant event in a workload's lifecycle.
// Events form an audit trail of the agent's decision-making process.
type WorkloadEvent struct {
	ID         string    `json:"id"`
	WorkloadID string    `json:"workload_id"`
	EventType  string    `json:"event_type"` // e.g., "state_change", "discovery", "bid_evaluation", "payment_auth"
	Details    string    `json:"details"`    // JSON-encoded event details
	CreatedAt  time.Time `json:"created_at"`
}
