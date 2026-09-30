package testing

import (
	"context"
	"encoding/json"

	"github.com/lollapalooza-ai/beacon/pkg/llm"
)

// MockLLMProvider is a mock LLM provider for testing that returns
// predetermined responses.
type MockLLMProvider struct {
	name      string
	responses []*llm.CompletionResponse
	callIndex int
	Error     error

	// Captured requests for assertions
	Requests []*llm.CompletionRequest
}

// NewMockLLMProvider creates a new mock LLM provider.
func NewMockLLMProvider(responses ...*llm.CompletionResponse) *MockLLMProvider {
	return &MockLLMProvider{
		name:      "mock",
		responses: responses,
	}
}

// Name returns "mock".
func (m *MockLLMProvider) Name() string {
	return m.name
}

// Complete returns the next preset response or an error.
func (m *MockLLMProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.Requests = append(m.Requests, req)

	if m.Error != nil {
		return nil, m.Error
	}

	if m.callIndex >= len(m.responses) {
		// If we run out of responses, return the last one
		return m.responses[len(m.responses)-1], nil
	}

	resp := m.responses[m.callIndex]
	m.callIndex++
	return resp, nil
}

// MakeIntentResponse creates a mock CompletionResponse for intent parsing.
func MakeIntentResponse(reqs *llm.ComputeRequirements) *llm.CompletionResponse {
	data, _ := json.Marshal(reqs)
	return &llm.CompletionResponse{
		Content:          string(data),
		StructuredOutput: data,
		Provider:         "mock",
		Model:            "mock-model",
	}
}

// MakeBidResponse creates a mock CompletionResponse for bid ranking.
func MakeBidResponse(ranking *llm.BidRanking) *llm.CompletionResponse {
	data, _ := json.Marshal(ranking)
	return &llm.CompletionResponse{
		Content:          string(data),
		StructuredOutput: data,
		Provider:         "mock",
		Model:            "mock-model",
	}
}
