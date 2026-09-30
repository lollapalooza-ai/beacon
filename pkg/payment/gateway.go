package payment

import "context"

// Gateway defines the interface for the payment and tokenization system.
// The gateway issues scoped, single-use tokens instead of handling raw
// payment credentials.
type Gateway interface {
	// AuthorizeBudget requests a new scoped payment token.
	// The token is tied to a specific budget, merchant, and purpose.
	AuthorizeBudget(ctx context.Context, req *AuthorizationRequest) (*AuthorizationToken, error)

	// CommitTransaction records a spend against an authorization token.
	CommitTransaction(ctx context.Context, tokenID string, amount float64) error

	// RevokeToken invalidates an authorization token, preventing further spend.
	RevokeToken(ctx context.Context, tokenID string) error

	// GetToken retrieves the current state of an authorization token.
	GetToken(ctx context.Context, tokenID string) (*AuthorizationToken, error)
}
