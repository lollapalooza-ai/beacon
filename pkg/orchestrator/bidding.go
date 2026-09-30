package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lollapalooza-ai/beacon/pkg/cloud"
	"github.com/lollapalooza-ai/beacon/pkg/llm"
)

const biddingSystemPrompt = `You are a cloud infrastructure cost optimization expert.
Your task is to evaluate and rank spot instance pricing options against a user's compute requirements.
Score each option from 0 to 100 based on:
1. Price (lower is better, assuming requirements are met)
2. Risk of interruption (evaluate based on instance type and typical provider spot stability)
3. Fit for the workload (don't over-provision unnecessarily, but ensure minimums are met)

Provide a detailed reasoning for each score, and assign a risk level (low, medium, high).
Finally, select the best overall option (selected_index) and provide a justification.
`

// EvaluateBids uses the LLM to rank spot pricing options based on compute requirements.
func EvaluateBids(ctx context.Context, provider llm.Provider, model string, reqs *llm.ComputeRequirements, bids []cloud.SpotPrice) (*llm.BidRanking, error) {
	if len(bids) == 0 {
		return nil, fmt.Errorf("no bids to evaluate")
	}

	var promptBuilder strings.Builder
	promptBuilder.WriteString("Compute Requirements:\n")
	reqJSON, _ := json.MarshalIndent(reqs, "", "  ")
	promptBuilder.WriteString(string(reqJSON))
	promptBuilder.WriteString("\n\nAvailable Spot Options:\n")
	
	for i, bid := range bids {
		promptBuilder.WriteString(fmt.Sprintf("[%d] Provider: %s, Region: %s, Zone: %s, Instance: %s, Price/hr: $%.4f, GPUs: %d %s, vCPUs: %d, Mem: %.1f GiB\n",
			i, bid.Provider, bid.Region, bid.Zone, bid.InstanceType, bid.PricePerHour, bid.GPUCount, bid.GPUType, bid.VCPUs, bid.MemoryGiB))
	}
	
	promptBuilder.WriteString("\nPlease evaluate these options and return the ranking in the requested JSON format.")

	req := &llm.CompletionRequest{
		Model:          model,
		SystemPrompt:   biddingSystemPrompt,
		Messages:       []llm.Message{{Role: llm.RoleUser, Content: promptBuilder.String()}},
		ResponseSchema: llm.BidRankingSchema(),
		Temperature:    0.0,
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	if len(resp.StructuredOutput) == 0 {
		return nil, fmt.Errorf("LLM returned empty structured output")
	}

	var ranking llm.BidRanking
	if err := json.Unmarshal(resp.StructuredOutput, &ranking); err != nil {
		return nil, fmt.Errorf("failed to unmarshal structured output: %w", err)
	}
	
	if ranking.SelectedIdx < 0 || ranking.SelectedIdx >= len(bids) {
		return nil, fmt.Errorf("LLM selected invalid index: %d", ranking.SelectedIdx)
	}

	return &ranking, nil
}
