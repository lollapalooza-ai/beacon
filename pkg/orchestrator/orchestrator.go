package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/lollapalooza-ai/beacon/pkg/cloud"
	"github.com/lollapalooza-ai/beacon/pkg/config"
	"github.com/lollapalooza-ai/beacon/pkg/llm"
	"github.com/lollapalooza-ai/beacon/pkg/payment"
	"github.com/lollapalooza-ai/beacon/pkg/state"
)

// Options holds dependencies for the Orchestrator.
type Options struct {
	LLMProvider   llm.Provider
	CloudAdapters map[string]cloud.Adapter
	PaymentGW     payment.Gateway
	Store         state.Store
	Config        *config.Config
	Logger        zerolog.Logger
}

// Orchestrator coordinates the workload lifecycle across cloud providers, LLMs, and payment gateways.
type Orchestrator struct {
	llmProvider   llm.Provider
	cloudAdapters map[string]cloud.Adapter
	paymentGw     payment.Gateway
	store         state.Store
	cfg           *config.Config
	logger        zerolog.Logger
}

// New creates a new Orchestrator instance.
func New(opts Options) (*Orchestrator, error) {
	if opts.LLMProvider == nil {
		return nil, fmt.Errorf("LLM provider is required")
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("state store is required")
	}
	if opts.PaymentGW == nil {
		return nil, fmt.Errorf("payment gateway is required")
	}
	if opts.Config == nil {
		return nil, fmt.Errorf("config is required")
	}

	return &Orchestrator{
		llmProvider:   opts.LLMProvider,
		cloudAdapters: opts.CloudAdapters,
		paymentGw:     opts.PaymentGW,
		store:         opts.Store,
		cfg:           opts.Config,
		logger:        opts.Logger,
	}, nil
}

// RunWorkload manages the entire lifecycle of a workload based on a natural language intent.
func (o *Orchestrator) RunWorkload(ctx context.Context, intent string, budget float64) (*state.Workload, error) {
	workload := &state.Workload{
		ID:        uuid.NewString(),
		Intent:    intent,
		State:     state.StatePending,
		Budget:    budget,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := o.store.CreateWorkload(ctx, workload); err != nil {
		return nil, fmt.Errorf("failed to create workload: %w", err)
	}

	o.transitionState(ctx, workload, state.StateDiscovering, "Started discovery phase")

	var lastErr error
	var reqs *llm.ComputeRequirements

	// Phase 1: Discovering
	for attempt := 1; attempt <= o.cfg.MaxDiscoveryRetries; attempt++ {
		r, err := ParseIntent(ctx, o.llmProvider, o.cfg.LLMModel, workload.Intent)
		if err != nil {
			lastErr = err
			o.logger.Warn().Err(err).Int("attempt", attempt).Msg("Failed to parse intent")
			continue
		}
		reqs = r
		break
	}
	if reqs == nil {
		return o.failWorkload(ctx, workload, fmt.Errorf("failed to parse intent after retries: %w", lastErr))
	}

	reqBytes, _ := json.Marshal(reqs)
	workload.Requirements = reqBytes
	o.store.UpdateWorkload(ctx, workload)
	o.logger.Info().RawJSON("requirements", reqBytes).Msg("Successfully parsed intent")

	var allBids []cloud.SpotPrice
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)

	spotReq := &cloud.SpotPriceRequest{
		GPUType:         reqs.GPUType,
		MinGPUs:         reqs.GPUCount,
		MinVCPUs:        reqs.VCPUs,
		MinMemoryGiB:    float64(reqs.MemoryGiB),
		MaxPricePerHour: reqs.MaxBudgetUSD,
	}

	for name, adapter := range o.cloudAdapters {
		name, adapter := name, adapter
		g.Go(func() error {
			bids, err := adapter.QuerySpotPrices(gCtx, spotReq)
			if err != nil {
				o.logger.Warn().Err(err).Str("provider", name).Msg("Failed to query spot prices")
				return nil // don't fail the whole group
			}
			mu.Lock()
			allBids = append(allBids, bids...)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	if len(allBids) == 0 {
		return o.failWorkload(ctx, workload, fmt.Errorf("no spot instances found matching requirements"))
	}

	// Phase 2: Bidding
	o.transitionState(ctx, workload, state.StateBidding, fmt.Sprintf("Evaluating %d spot bids", len(allBids)))
	
	ranking, err := EvaluateBids(ctx, o.llmProvider, o.cfg.LLMModel, reqs, allBids)
	if err != nil {
		return o.failWorkload(ctx, workload, fmt.Errorf("failed to evaluate bids: %w", err))
	}

	selectedBid := allBids[ranking.SelectedIdx]
	selectedBidBytes, _ := json.Marshal(selectedBid)
	workload.SelectedOffer = selectedBidBytes
	o.store.UpdateWorkload(ctx, workload)

	o.logger.Info().
		Str("provider", selectedBid.Provider).
		Str("instance_type", selectedBid.InstanceType).
		Float64("price", selectedBid.PricePerHour).
		Str("justification", ranking.Justification).
		Msg("Selected spot bid")

	// Phase 3: Authorizing
	o.transitionState(ctx, workload, state.StateAuthorizing, "Authorizing payment")
	
	authReq := &payment.AuthorizationRequest{
		WorkloadID:    workload.ID,
		Budget:        budget,
		Merchant:      selectedBid.Provider,
		Purpose:       fmt.Sprintf("Spot instance %s in %s", selectedBid.InstanceType, selectedBid.Region),
		DurationHours: reqs.MaxDurationH,
	}

	token, err := o.paymentGw.AuthorizeBudget(ctx, authReq)
	if err != nil {
		return o.failWorkload(ctx, workload, fmt.Errorf("payment authorization failed: %w", err))
	}
	workload.PaymentTokenID = token.TokenID
	o.store.UpdateWorkload(ctx, workload)

	// Phase 4: Provisioning
	o.transitionState(ctx, workload, state.StateProvisioning, "Provisioning instance")
	
	adapter, ok := o.cloudAdapters[selectedBid.Provider]
	if !ok {
		return o.failWorkload(ctx, workload, fmt.Errorf("no adapter found for provider: %s", selectedBid.Provider))
	}

	provReq := &cloud.ProvisionRequest{
		InstanceType: selectedBid.InstanceType,
		Region:       selectedBid.Region,
		Zone:         selectedBid.Zone,
		MaxPrice:     selectedBid.PricePerHour,
		StorageGiB:   reqs.StorageGiB,
		Tags:         map[string]string{"WorkloadID": workload.ID},
	}

	var instance *cloud.Instance
	for attempt := 1; attempt <= o.cfg.MaxProvisionRetries; attempt++ {
		instance, err = adapter.ProvisionInstance(ctx, provReq)
		if err == nil {
			break
		}
		lastErr = err
		o.logger.Warn().Err(err).Int("attempt", attempt).Msg("Failed to provision instance")
	}

	if instance == nil {
		return o.failWorkload(ctx, workload, fmt.Errorf("failed to provision after retries: %w", lastErr))
	}

	workload.InstanceID = instance.ID
	workload.InstanceProvider = instance.Provider
	workload.InstanceRegion = instance.Region
	o.store.UpdateWorkload(ctx, workload)

	// Phase 5: Running
	o.transitionState(ctx, workload, state.StateRunning, "Instance running")
	
	// Start monitoring loop (blocks until context is done or workload finishes)
	err = o.monitorInstance(ctx, adapter, workload)
	
	// Phase 6: Decommissioning (either on success or failure of workload)
	o.transitionState(ctx, workload, state.StateDecommissioning, "Decommissioning instance")
	
	termErr := adapter.TerminateInstance(context.Background(), workload.InstanceID, workload.InstanceRegion)
	if termErr != nil {
		o.logger.Error().Err(termErr).Str("instance", workload.InstanceID).Msg("Failed to terminate instance")
	}
	
	revErr := o.paymentGw.RevokeToken(context.Background(), workload.PaymentTokenID)
	if revErr != nil {
		o.logger.Error().Err(revErr).Str("token", workload.PaymentTokenID).Msg("Failed to revoke token")
	}

	if err != nil {
		o.transitionState(ctx, workload, state.StateFailed, fmt.Sprintf("Workload failed: %v", err))
		workload.Error = err.Error()
	} else {
		o.transitionState(ctx, workload, state.StateCompleted, "Workload completed successfully")
	}
	
	o.store.UpdateWorkload(ctx, workload)
	return workload, err
}

func (o *Orchestrator) monitorInstance(ctx context.Context, adapter cloud.Adapter, workload *state.Workload) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := adapter.GetInstanceStatus(ctx, workload.InstanceID, workload.InstanceRegion)
			if err != nil {
				o.logger.Warn().Err(err).Str("instance", workload.InstanceID).Msg("Failed to get instance status")
				continue
			}
			
			if status.Instance.State == cloud.InstanceStateTerminated || status.Instance.State == cloud.InstanceStateTerminating {
				o.logger.Info().Str("instance", workload.InstanceID).Msg("Instance terminated externally")
				return fmt.Errorf("instance terminated unexpectedly")
			}
			
			if !status.HealthCheck {
				o.logger.Warn().Str("instance", workload.InstanceID).Msg("Instance health check failed")
			}
			
			// Here you would check if workload is complete (e.g. via SSH/agent on instance).
			// For now, we'll assume it runs until context is cancelled or another external trigger.
			// The orchestrator typically gets a signal when a workload is done.
		}
	}
}

func (o *Orchestrator) failWorkload(ctx context.Context, workload *state.Workload, err error) (*state.Workload, error) {
	o.logger.Error().Err(err).Str("workload", workload.ID).Msg("Workload failed")
	workload.Error = err.Error()
	o.transitionState(ctx, workload, state.StateFailed, err.Error())
	
	// Best-effort cleanup
	if workload.InstanceID != "" {
		if adapter, ok := o.cloudAdapters[workload.InstanceProvider]; ok {
			_ = adapter.TerminateInstance(context.Background(), workload.InstanceID, workload.InstanceRegion)
		}
	}
	if workload.PaymentTokenID != "" {
		_ = o.paymentGw.RevokeToken(context.Background(), workload.PaymentTokenID)
	}
	
	return workload, err
}

func (o *Orchestrator) transitionState(ctx context.Context, workload *state.Workload, newState state.WorkloadState, details string) {
	o.logger.Info().Str("workload", workload.ID).Str("old_state", string(workload.State)).Str("new_state", string(newState)).Msg("State transition")
	
	workload.State = newState
	workload.UpdatedAt = time.Now()
	
	_ = o.store.UpdateWorkload(ctx, workload)
	
	event := &state.WorkloadEvent{
		ID:         uuid.NewString(),
		WorkloadID: workload.ID,
		EventType:  "state_change",
		Details:    fmt.Sprintf(`{"state": "%s", "message": "%s"}`, newState, details),
		CreatedAt:  time.Now(),
	}
	_ = o.store.RecordEvent(ctx, event)
}

// RecoverOrphanedWorkloads recovers active workloads from the store on startup.
func (o *Orchestrator) RecoverOrphanedWorkloads(ctx context.Context) error {
	workloads, err := o.store.ListActiveWorkloads(ctx)
	if err != nil {
		return fmt.Errorf("failed to list active workloads: %w", err)
	}

	for _, w := range workloads {
		if w.State == state.StateProvisioning || w.State == state.StateRunning || w.State == state.StateDecommissioning {
			o.logger.Info().Str("workload", w.ID).Str("state", string(w.State)).Msg("Recovering orphaned workload")
			
			o.transitionState(ctx, w, state.StateDecommissioning, "Recovering orphaned workload on startup")
			
			if w.InstanceID != "" && w.InstanceProvider != "" {
				if adapter, ok := o.cloudAdapters[w.InstanceProvider]; ok {
					if err := adapter.TerminateInstance(ctx, w.InstanceID, w.InstanceRegion); err != nil {
						o.logger.Error().Err(err).Str("instance", w.InstanceID).Msg("Failed to terminate orphaned instance")
					}
				}
			}
			
			if w.PaymentTokenID != "" {
				if err := o.paymentGw.RevokeToken(ctx, w.PaymentTokenID); err != nil {
					o.logger.Error().Err(err).Str("token", w.PaymentTokenID).Msg("Failed to revoke token for orphaned workload")
				}
			}
			
			o.transitionState(ctx, w, state.StateCompleted, "Cleaned up orphaned workload")
		}
	}

	return nil
}
