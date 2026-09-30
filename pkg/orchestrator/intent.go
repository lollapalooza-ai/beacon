package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lollapalooza-ai/beacon/pkg/llm"
)

const intentSystemPrompt = `You are a cloud infrastructure requirements parser.
Your task is to extract structured compute requirements from a user's natural language intent.

Given a workload description, extract the following:
- GPU type and count (if mentioned)
- CPU and memory requirements
- Storage requirements
- Workload type: "training" (ML training), "inference" (model serving), "batch" (data processing), or "general"
- Budget constraints
- Duration constraints
- Preferred regions or OS

Guidelines:
- If the user mentions a specific GPU (e.g., "A100", "H100"), set gpu_type accordingly.
- If the user says "4xA100", set gpu_count to 4 and gpu_type to "A100".
- Common inference: "Train ResNet on 4xA100 under $50" → workload_type=training, gpu_type=A100, gpu_count=4, max_budget_usd=50
- If budget is mentioned as "under $X" or "less than $X", use X as max_budget_usd.
- If duration is mentioned as "for 2 hours", set max_duration_hours to 2.
- If no GPU is mentioned but the task seems compute-heavy, estimate reasonable CPU/memory.
- If no budget is explicitly stated, set max_budget_usd to 0 (the orchestrator will use the global budget).
- Always set workload_type based on the nature of the task.

Examples:
- "Train ResNet on 4xA100 under $50 for 3 hours" → gpu_type=A100, gpu_count=4, workload_type=training, max_budget_usd=50, max_duration_hours=3
- "Run inference on a T4 GPU in us-west-2" → gpu_type=T4, gpu_count=1, workload_type=inference, preferred_regions=["us-west-2"]
- "Batch process 10TB of data, need at least 64 vCPUs and 256GB RAM, budget $200" → vcpus=64, memory_gib=256, workload_type=batch, max_budget_usd=200
- "Deploy a web service on a cheap spot instance" → workload_type=general, vcpus=2, memory_gib=4
`

// ParseIntent uses the LLM to parse a natural language intent string into ComputeRequirements.
func ParseIntent(ctx context.Context, provider llm.Provider, model string, intent string) (*llm.ComputeRequirements, error) {
	req := &llm.CompletionRequest{
		Model:        model,
		SystemPrompt: intentSystemPrompt,
		Messages: []llm.Message{
			{
				Role:    llm.RoleUser,
				Content: fmt.Sprintf("Parse the following workload intent into compute requirements:\n\n%s", intent),
			},
		},
		ResponseSchema: llm.IntentParseSchema(),
		Temperature:    0.0, // deterministic parsing
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed during intent parsing: %w", err)
	}

	// Use StructuredOutput if available, otherwise try parsing Content
	outputJSON := resp.StructuredOutput
	if len(outputJSON) == 0 {
		if resp.Content == "" {
			return nil, fmt.Errorf("LLM returned empty response for intent parsing")
		}
		outputJSON = json.RawMessage(resp.Content)
	}

	var reqs llm.ComputeRequirements
	if err := json.Unmarshal(outputJSON, &reqs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal compute requirements from LLM output: %w (raw: %.500s)", err, string(outputJSON))
	}

	// Validate minimum required fields
	if reqs.WorkloadType == "" {
		reqs.WorkloadType = "general"
	}

	return &reqs, nil
}
