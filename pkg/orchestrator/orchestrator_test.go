package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/lollapalooza-ai/beacon/pkg/cloud"
	"github.com/lollapalooza-ai/beacon/pkg/config"
	"github.com/lollapalooza-ai/beacon/pkg/llm"
	"github.com/lollapalooza-ai/beacon/pkg/payment"
	"github.com/lollapalooza-ai/beacon/pkg/state"

	mocktesting "github.com/lollapalooza-ai/beacon/internal/testing"
)

func setupTestOrchestrator(t *testing.T, mockLLM *mocktesting.MockLLMProvider, mockCloud *mocktesting.MockCloudAdapter) (*Orchestrator, state.Store) {
	t.Helper()

	dbPath := t.TempDir() + "/test.db"
	logger := zerolog.Nop()
	store, err := state.NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.MaxProvisionRetries = 2
	cfg.MaxDiscoveryRetries = 2

	adapters := map[string]cloud.Adapter{
		mockCloud.Name(): mockCloud,
	}

	orch, err := New(Options{
		LLMProvider:   mockLLM,
		CloudAdapters: adapters,
		PaymentGW:     payment.NewStubGateway(logger),
		Store:         store,
		Config:        cfg,
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	return orch, store
}

func defaultMockPrices() []cloud.SpotPrice {
	return []cloud.SpotPrice{
		{
			Provider:     "mock-aws",
			Region:       "us-east-1",
			Zone:         "us-east-1a",
			InstanceType: "p3.2xlarge",
			PricePerHour: 1.50,
			GPUType:      "V100",
			GPUCount:     1,
			VCPUs:        8,
			MemoryGiB:    61,
			Timestamp:    time.Now(),
		},
		{
			Provider:     "mock-aws",
			Region:       "us-west-2",
			Zone:         "us-west-2b",
			InstanceType: "g4dn.xlarge",
			PricePerHour: 0.35,
			GPUType:      "T4",
			GPUCount:     1,
			VCPUs:        4,
			MemoryGiB:    16,
			Timestamp:    time.Now(),
		},
	}
}

func TestRunWorkload_FullLifecycle(t *testing.T) {
	intentResp := mocktesting.MakeIntentResponse(&llm.ComputeRequirements{
		GPUType:      "V100",
		GPUCount:     1,
		WorkloadType: "training",
		MaxBudgetUSD: 50.0,
		MaxDurationH: 2.0,
	})

	bidResp := mocktesting.MakeBidResponse(&llm.BidRanking{
		Rankings: []llm.BidEvaluation{
			{InstanceType: "p3.2xlarge", Region: "us-east-1", Provider: "mock-aws", Score: 85, Reasoning: "Good fit", RiskLevel: "low", EstimatedCost: 3.0},
			{InstanceType: "g4dn.xlarge", Region: "us-west-2", Provider: "mock-aws", Score: 60, Reasoning: "Under-provisioned", RiskLevel: "medium", EstimatedCost: 0.70},
		},
		SelectedIdx:   0,
		Justification: "p3.2xlarge is the best fit for V100 training",
	})

	mockLLM := mocktesting.NewMockLLMProvider(intentResp, bidResp)
	mockCloud := mocktesting.NewMockCloudAdapter("mock-aws", defaultMockPrices())

	orch, store := setupTestOrchestrator(t, mockLLM, mockCloud)
	defer store.Close()

	// Use a short-lived context to avoid blocking on the monitor loop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	workload, err := orch.RunWorkload(ctx, "Train ResNet on V100 under $50", 50.0)
	if err != nil && err != context.DeadlineExceeded {
		// Context deadline is expected since the monitor loop runs until cancelled
		t.Fatalf("unexpected error: %v", err)
	}

	if workload == nil {
		t.Fatal("expected workload to be non-nil")
	}

	// Verify the workload went through the expected states
	if workload.InstanceID == "" {
		t.Error("expected instance to be provisioned")
	}

	// Verify LLM was called twice (intent + bidding)
	if len(mockLLM.Requests) != 2 {
		t.Errorf("expected 2 LLM calls, got %d", len(mockLLM.Requests))
	}

	// Verify cloud adapter was called
	if mockCloud.QueryCount == 0 {
		t.Error("expected spot price query to be called")
	}
	if mockCloud.ProvisionCount == 0 {
		t.Error("expected provision to be called")
	}

	// Verify termination happened (decommission on context cancel)
	if mockCloud.TerminateCount == 0 {
		t.Error("expected terminate to be called during decommission")
	}
}

func TestRunWorkload_NoBidsFound(t *testing.T) {
	intentResp := mocktesting.MakeIntentResponse(&llm.ComputeRequirements{
		GPUType:      "H100", // no H100s in our mock prices
		GPUCount:     8,
		WorkloadType: "training",
		MaxBudgetUSD: 10.0,
	})

	mockLLM := mocktesting.NewMockLLMProvider(intentResp)
	mockCloud := mocktesting.NewMockCloudAdapter("mock-aws", defaultMockPrices())

	orch, store := setupTestOrchestrator(t, mockLLM, mockCloud)
	defer store.Close()

	ctx := context.Background()
	workload, err := orch.RunWorkload(ctx, "Train on 8xH100 under $10", 10.0)

	if err == nil {
		t.Fatal("expected error when no bids match")
	}

	if workload == nil {
		t.Fatal("expected workload to be returned even on failure")
	}

	if workload.State != state.StateFailed {
		t.Errorf("expected state Failed, got %s", workload.State)
	}
}

func TestRunWorkload_ProvisionRetryExhaustion(t *testing.T) {
	intentResp := mocktesting.MakeIntentResponse(&llm.ComputeRequirements{
		WorkloadType: "general",
		MaxBudgetUSD: 50.0,
	})

	bidResp := mocktesting.MakeBidResponse(&llm.BidRanking{
		Rankings:      []llm.BidEvaluation{{InstanceType: "p3.2xlarge", Region: "us-east-1", Provider: "mock-aws", Score: 80, Reasoning: "ok", RiskLevel: "low", EstimatedCost: 3.0}},
		SelectedIdx:   0,
		Justification: "only option",
	})

	mockLLM := mocktesting.NewMockLLMProvider(intentResp, bidResp)
	mockCloud := mocktesting.NewMockCloudAdapter("mock-aws", defaultMockPrices())
	mockCloud.ProvisionError = context.DeadlineExceeded // simulate persistent failure

	orch, store := setupTestOrchestrator(t, mockLLM, mockCloud)
	defer store.Close()

	workload, err := orch.RunWorkload(context.Background(), "Run a general workload", 50.0)
	if err == nil {
		t.Fatal("expected error on provision failure")
	}

	if workload.State != state.StateFailed {
		t.Errorf("expected state Failed, got %s", workload.State)
	}

	// Should have retried MaxProvisionRetries times
	if mockCloud.ProvisionCount != 2 {
		t.Errorf("expected 2 provision attempts, got %d", mockCloud.ProvisionCount)
	}
}

func TestRunWorkload_IntentParseRetryExhaustion(t *testing.T) {
	mockLLM := mocktesting.NewMockLLMProvider()
	mockLLM.Error = context.DeadlineExceeded // all LLM calls fail

	mockCloud := mocktesting.NewMockCloudAdapter("mock-aws", defaultMockPrices())

	orch, store := setupTestOrchestrator(t, mockLLM, mockCloud)
	defer store.Close()

	workload, err := orch.RunWorkload(context.Background(), "Train something", 50.0)
	if err == nil {
		t.Fatal("expected error on intent parse failure")
	}

	if workload.State != state.StateFailed {
		t.Errorf("expected state Failed, got %s", workload.State)
	}

	// Should have retried MaxDiscoveryRetries times
	if len(mockLLM.Requests) != 2 {
		t.Errorf("expected 2 LLM attempts, got %d", len(mockLLM.Requests))
	}
}

func TestRecoverOrphanedWorkloads(t *testing.T) {
	mockLLM := mocktesting.NewMockLLMProvider()
	mockCloud := mocktesting.NewMockCloudAdapter("mock-aws", nil)

	// Pre-provision a mock instance so terminate works
	mockCloud.ProvisionInstance(context.Background(), &cloud.ProvisionRequest{
		InstanceType: "p3.2xlarge",
		Region:       "us-east-1",
	})

	orch, store := setupTestOrchestrator(t, mockLLM, mockCloud)
	defer store.Close()

	// Simulate an orphaned workload in the store
	orphan := &state.Workload{
		ID:               "orphan-1",
		Intent:           "Orphaned workload",
		State:            state.StateRunning,
		InstanceID:       "i-mock-1",
		InstanceProvider: "mock-aws",
		InstanceRegion:   "us-east-1",
		PaymentTokenID:   "token-1",
		Budget:           100.0,
	}
	if err := store.CreateWorkload(context.Background(), orphan); err != nil {
		t.Fatalf("failed to create orphan: %v", err)
	}

	err := orch.RecoverOrphanedWorkloads(context.Background())
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	// Verify instance was terminated
	if mockCloud.TerminateCount == 0 {
		t.Error("expected orphaned instance to be terminated")
	}

	// Verify workload was marked completed
	recovered, err := store.GetWorkload(context.Background(), "orphan-1")
	if err != nil {
		t.Fatalf("failed to get recovered workload: %v", err)
	}
	if recovered.State != state.StateCompleted {
		t.Errorf("expected recovered workload to be completed, got %s", recovered.State)
	}
}

func TestNew_MissingDependencies(t *testing.T) {
	_, err := New(Options{})
	if err == nil {
		t.Fatal("expected error when LLM provider is nil")
	}
}
