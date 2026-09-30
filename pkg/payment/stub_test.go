package payment

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

func TestStubGateway_AuthorizeBudget(t *testing.T) {
	gw := NewStubGateway(zerolog.Nop())

	token, err := gw.AuthorizeBudget(context.Background(), &AuthorizationRequest{
		WorkloadID:    "w-1",
		Budget:        100.0,
		Merchant:      "aws",
		Purpose:       "Spot instance",
		DurationHours: 2.0,
	})
	if err != nil {
		t.Fatalf("AuthorizeBudget failed: %v", err)
	}

	if token.TokenID == "" {
		t.Error("expected non-empty token ID")
	}
	if token.Budget != 100.0 {
		t.Errorf("expected budget 100, got %f", token.Budget)
	}
	if token.Merchant != "aws" {
		t.Errorf("expected merchant aws, got %s", token.Merchant)
	}
	if token.Revoked {
		t.Error("expected token to not be revoked")
	}
}

func TestStubGateway_CommitTransaction(t *testing.T) {
	gw := NewStubGateway(zerolog.Nop())

	token, _ := gw.AuthorizeBudget(context.Background(), &AuthorizationRequest{
		WorkloadID:    "w-1",
		Budget:        50.0,
		Merchant:      "aws",
		DurationHours: 1.0,
	})

	// Should succeed within budget
	err := gw.CommitTransaction(context.Background(), token.TokenID, 30.0)
	if err != nil {
		t.Fatalf("CommitTransaction failed: %v", err)
	}

	// Should succeed (30 + 15 = 45 < 50)
	err = gw.CommitTransaction(context.Background(), token.TokenID, 15.0)
	if err != nil {
		t.Fatalf("CommitTransaction failed: %v", err)
	}

	// Should fail (45 + 10 = 55 > 50)
	err = gw.CommitTransaction(context.Background(), token.TokenID, 10.0)
	if err == nil {
		t.Fatal("expected error when exceeding budget")
	}
}

func TestStubGateway_RevokeToken(t *testing.T) {
	gw := NewStubGateway(zerolog.Nop())

	token, _ := gw.AuthorizeBudget(context.Background(), &AuthorizationRequest{
		WorkloadID: "w-1",
		Budget:     100.0,
		Merchant:   "aws",
	})

	err := gw.RevokeToken(context.Background(), token.TokenID)
	if err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	// Commit should fail after revocation
	err = gw.CommitTransaction(context.Background(), token.TokenID, 10.0)
	if err == nil {
		t.Fatal("expected error committing to revoked token")
	}
}

func TestStubGateway_GetToken(t *testing.T) {
	gw := NewStubGateway(zerolog.Nop())

	token, _ := gw.AuthorizeBudget(context.Background(), &AuthorizationRequest{
		WorkloadID: "w-1",
		Budget:     75.0,
		Merchant:   "gcp",
	})

	got, err := gw.GetToken(context.Background(), token.TokenID)
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}

	if got.Budget != 75.0 {
		t.Errorf("expected budget 75, got %f", got.Budget)
	}

	// Should return error for unknown token
	_, err = gw.GetToken(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token")
	}
}
