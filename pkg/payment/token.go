// Package payment provides the payment and tokenization gateway interface.
// The gateway issues scoped, single-use cryptographic tokens for authorizing
// cloud compute purchases without handling raw payment credentials.
package payment

import "time"

// AuthorizationToken represents a single-use, scoped payment token.
type AuthorizationToken struct {
	// TokenID is the unique identifier for this token.
	TokenID string `json:"token_id"`

	// Budget is the maximum authorized spend in USD.
	Budget float64 `json:"budget_usd"`

	// Merchant is the scoped merchant (e.g., "aws", "gcp").
	Merchant string `json:"merchant"`

	// Purpose describes what this token authorizes.
	Purpose string `json:"purpose"`

	// ExpiresAt is when the token expires.
	ExpiresAt time.Time `json:"expires_at"`

	// CreatedAt is when the token was created.
	CreatedAt time.Time `json:"created_at"`

	// Revoked indicates whether this token has been revoked.
	Revoked bool `json:"revoked"`

	// AmountCommitted tracks how much has been committed against this token.
	AmountCommitted float64 `json:"amount_committed"`
}

// AuthorizationRequest contains the parameters for requesting a payment token.
type AuthorizationRequest struct {
	// WorkloadID ties this authorization to a specific workload.
	WorkloadID string `json:"workload_id"`

	// Budget is the requested maximum spend in USD.
	Budget float64 `json:"budget_usd"`

	// Merchant is the cloud provider to scope the token to.
	Merchant string `json:"merchant"`

	// Purpose describes the authorization purpose.
	Purpose string `json:"purpose"`

	// DurationHours is how long the token should remain valid.
	DurationHours float64 `json:"duration_hours"`
}
