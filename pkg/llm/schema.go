package llm

import "encoding/json"

// ComputeRequirements represents the parsed compute requirements from a workload intent.
type ComputeRequirements struct {
	// GPU requirements
	GPUType  string `json:"gpu_type,omitempty"`  // e.g., "A100", "H100", "T4"
	GPUCount int    `json:"gpu_count,omitempty"` // number of GPUs needed

	// CPU requirements
	VCPUs     int `json:"vcpus,omitempty"`      // minimum vCPUs
	MemoryGiB int `json:"memory_gib,omitempty"` // minimum memory in GiB

	// Storage
	StorageGiB  int    `json:"storage_gib,omitempty"`  // disk space in GiB
	StorageType string `json:"storage_type,omitempty"` // "ssd", "hdd", "nvme"

	// Workload metadata
	WorkloadType string  `json:"workload_type"`      // "training", "inference", "batch", "general"
	MaxBudgetUSD float64 `json:"max_budget_usd"`     // maximum spend for this workload
	MaxDurationH float64 `json:"max_duration_hours"` // maximum runtime in hours

	// Preferences
	PreferredRegions []string `json:"preferred_regions,omitempty"` // e.g., ["us-east-1", "us-west-2"]
	PreferredOS      string   `json:"preferred_os,omitempty"`      // e.g., "ubuntu-22.04"
}

// BidEvaluation represents the LLM's assessment of a spot pricing option.
type BidEvaluation struct {
	InstanceType  string  `json:"instance_type"`
	Region        string  `json:"region"`
	Provider      string  `json:"provider"`
	Score         float64 `json:"score"`          // 0-100 composite score
	Reasoning     string  `json:"reasoning"`      // explanation of the score
	RiskLevel     string  `json:"risk_level"`     // "low", "medium", "high"
	EstimatedCost float64 `json:"estimated_cost"` // estimated total cost in USD
}

// BidRanking contains the LLM's ranked list of spot options.
type BidRanking struct {
	Rankings      []BidEvaluation `json:"rankings"`
	SelectedIdx   int             `json:"selected_index"` // index of the recommended option
	Justification string          `json:"justification"`  // overall reasoning
}

// IntentParseSchema returns the JSON schema for parsing workload intents.
func IntentParseSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"gpu_type": {"type": "string", "description": "GPU type required (e.g., A100, H100, T4)"},
			"gpu_count": {"type": "integer", "description": "Number of GPUs needed"},
			"vcpus": {"type": "integer", "description": "Minimum number of vCPUs"},
			"memory_gib": {"type": "integer", "description": "Minimum memory in GiB"},
			"storage_gib": {"type": "integer", "description": "Required disk space in GiB"},
			"storage_type": {"type": "string", "enum": ["ssd", "hdd", "nvme"]},
			"workload_type": {"type": "string", "enum": ["training", "inference", "batch", "general"]},
			"max_budget_usd": {"type": "number", "description": "Maximum budget in USD"},
			"max_duration_hours": {"type": "number", "description": "Maximum runtime in hours"},
			"preferred_regions": {"type": "array", "items": {"type": "string"}},
			"preferred_os": {"type": "string"}
		},
		"required": ["workload_type", "max_budget_usd"]
	}`)
}

// BidRankingSchema returns the JSON schema for bid evaluation.
func BidRankingSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"rankings": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"instance_type": {"type": "string"},
						"region": {"type": "string"},
						"provider": {"type": "string"},
						"score": {"type": "number", "minimum": 0, "maximum": 100},
						"reasoning": {"type": "string"},
						"risk_level": {"type": "string", "enum": ["low", "medium", "high"]},
						"estimated_cost": {"type": "number"}
					},
					"required": ["instance_type", "region", "provider", "score", "reasoning", "risk_level", "estimated_cost"]
				}
			},
			"selected_index": {"type": "integer"},
			"justification": {"type": "string"}
		},
		"required": ["rankings", "selected_index", "justification"]
	}`)
}
