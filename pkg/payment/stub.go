package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// StubGateway provides an in-memory payment gateway for development and testing.
type StubGateway struct {
	logger zerolog.Logger
	mu     sync.RWMutex
	tokens map[string]*AuthorizationToken
}

// NewStubGateway creates a new StubGateway.
func NewStubGateway(logger zerolog.Logger) *StubGateway {
	return &StubGateway{
		logger: logger.With().Str("component", "stub_gateway").Logger(),
		tokens: make(map[string]*AuthorizationToken),
	}
}

// AuthorizeBudget requests a new scoped payment token.
func (g *StubGateway) AuthorizeBudget(ctx context.Context, req *AuthorizationRequest) (*AuthorizationToken, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tokenID := uuid.New().String()
	now := time.Now()
	expiresAt := now.Add(time.Duration(req.DurationHours * float64(time.Hour)))

	token := &AuthorizationToken{
		TokenID:         tokenID,
		Budget:          req.Budget,
		Merchant:        req.Merchant,
		Purpose:         req.Purpose,
		ExpiresAt:       expiresAt,
		CreatedAt:       now,
		Revoked:         false,
		AmountCommitted: 0,
	}

	g.tokens[tokenID] = token

	g.logger.Info().
		Str("workload_id", req.WorkloadID).
		Float64("budget", req.Budget).
		Str("merchant", req.Merchant).
		Str("token_id", tokenID).
		Msg("Authorized budget")

	return token, nil
}

// CommitTransaction records a spend against an authorization token.
func (g *StubGateway) CommitTransaction(ctx context.Context, tokenID string, amount float64) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	token, ok := g.tokens[tokenID]
	if !ok {
		return errors.New("token not found")
	}

	if token.Revoked {
		return errors.New("token is revoked")
	}

	if time.Now().After(token.ExpiresAt) {
		return errors.New("token is expired")
	}

	if token.AmountCommitted+amount > token.Budget {
		return fmt.Errorf("insufficient budget: requested %f, available %f", amount, token.Budget-token.AmountCommitted)
	}

	token.AmountCommitted += amount

	g.logger.Info().
		Str("token_id", tokenID).
		Float64("amount", amount).
		Float64("total_committed", token.AmountCommitted).
		Msg("Committed transaction")

	return nil
}

// RevokeToken invalidates an authorization token.
func (g *StubGateway) RevokeToken(ctx context.Context, tokenID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	token, ok := g.tokens[tokenID]
	if !ok {
		return errors.New("token not found")
	}

	token.Revoked = true
	g.logger.Info().Str("token_id", tokenID).Msg("Revoked token")

	return nil
}

// GetToken retrieves the current state of an authorization token.
func (g *StubGateway) GetToken(ctx context.Context, tokenID string) (*AuthorizationToken, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	token, ok := g.tokens[tokenID]
	if !ok {
		return nil, errors.New("token not found")
	}

	// Return a copy to prevent external modification
	tokenCopy := *token
	return &tokenCopy, nil
}
