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

func setupTestOrchestrator(t *testing.T, mockLLM *mocktesting.MockLLMProvider, mocks ...*mocktesting.MockCloudAdapter) (*Orchestrator, state.Store) {
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

	adapters := make(map[string]cloud.Adapter)
	for _, m := range mocks {
		adapters[m.Name()] = m
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

func defaultMockPricesAWS() []cloud.SpotPrice {
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
	}
}

func defaultMockPricesGCP() []cloud.SpotPrice {
	return []cloud.SpotPrice{
		{
			Provider:     "mock-gcp",
			Region:       "us-central1",
			Zone:         "us-central1-a",
			InstanceType: "a2-highgpu-1g",
			PricePerHour: 1.10, // Cheaper than AWS
			GPUType:      "A100",
			GPUCount:     1,
			VCPUs:        12,
			MemoryGiB:    85,
			Timestamp:    time.Now(),
		},
	}
}

func TestRunWorkload_FullLifecycle_MultiCloud(t *testing.T) {
	intentResp := mocktesting.MakeIntentResponse(&llm.ComputeRequirements{
		GPUType:      "", // empty means any GPU is fine
		GPUCount:     1,
		WorkloadType: "training",
		MaxBudgetUSD: 50.0,
		MaxDurationH: 2.0,
	})

	// The LLM evaluates both and picks GCP because it's cheaper and has a better GPU (A100 vs V100)
	bidResp := mocktesting.MakeBidResponse(&llm.BidRanking{
		Rankings: []llm.BidEvaluation{
			{InstanceType: "a2-highgpu-1g", Region: "us-central1", Provider: "mock-gcp", Score: 95, Reasoning: "Cheaper and faster", RiskLevel: "low", EstimatedCost: 2.20},
			{InstanceType: "p3.2xlarge", Region: "us-east-1", Provider: "mock-aws", Score: 70, Reasoning: "More expensive", RiskLevel: "low", EstimatedCost: 3.00},
		},
		SelectedIdx:   0, // Picks GCP
		Justification: "a2-highgpu-1g is the best fit",
	})

	mockLLM := mocktesting.NewMockLLMProvider(intentResp, bidResp)
	mockAWS := mocktesting.NewMockCloudAdapter("mock-aws", defaultMockPricesAWS())
	mockGCP := mocktesting.NewMockCloudAdapter("mock-gcp", defaultMockPricesGCP())

	orch, store := setupTestOrchestrator(t, mockLLM, mockAWS, mockGCP)
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	workload, err := orch.RunWorkload(ctx, "Train ResNet under $50", 50.0)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	if workload == nil {
		t.Fatal("expected workload to be non-nil")
	}
	if workload.InstanceProvider != "mock-gcp" {
		t.Errorf("expected instance to be provisioned on mock-gcp, got %s", workload.InstanceProvider)
	}

	// Verify both cloud adapters were queried
	if mockAWS.QueryCount == 0 {
		t.Error("expected AWS spot price query to be called")
	}
	if mockGCP.QueryCount == 0 {
		t.Error("expected GCP spot price query to be called")
	}

	// Verify only GCP was provisioned
	if mockAWS.ProvisionCount != 0 {
		t.Error("expected AWS provision to NOT be called")
	}
	if mockGCP.ProvisionCount == 0 {
		t.Error("expected GCP provision to be called")
	}

	// Verify termination happened on the correct adapter
	if mockGCP.TerminateCount == 0 {
		t.Error("expected terminate to be called on GCP during decommission")
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
	mockCloud := mocktesting.NewMockCloudAdapter("mock-aws", defaultMockPricesAWS())

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
	mockCloud := mocktesting.NewMockCloudAdapter("mock-aws", defaultMockPricesAWS())
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

	mockCloud := mocktesting.NewMockCloudAdapter("mock-aws", defaultMockPricesAWS())

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
