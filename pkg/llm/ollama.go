package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"
)

// OllamaProvider implements the Provider interface for Ollama local inference.
type OllamaProvider struct {
	endpoint string
	client   *http.Client
}

// NewOllamaProvider creates a new OllamaProvider.
// The endpoint should be the base Ollama URL (e.g., "http://localhost:11434").
func NewOllamaProvider(endpoint string) *OllamaProvider {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	return &OllamaProvider{
		endpoint: endpoint,
		client:   &http.Client{},
	}
}

// Name returns the provider's identifier.
func (p *OllamaProvider) Name() string {
	return "ollama"
}

// ollamaMessage mirrors the Ollama API message format.
type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaChatRequest is the request body for POST /api/chat.
type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   json.RawMessage `json:"format,omitempty"` // JSON schema for structured output
	Options  *ollamaOptions  `json:"options,omitempty"`
}

// ollamaOptions provides model parameters for the request.
type ollamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"` // max tokens
}

// ollamaChatResponse is the response body from POST /api/chat.
type ollamaChatResponse struct {
	Model   string        `json:"model"`
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`

	// Token usage fields (only populated when done=true)
	PromptEvalCount   int `json:"prompt_eval_count"`
	EvalCount         int `json:"eval_count"`
	TotalDuration     int `json:"total_duration"`
	LoadDuration      int `json:"load_duration"`
	PromptEvalDuration int `json:"prompt_eval_duration"`
	EvalDuration      int `json:"eval_duration"`
}

// Complete sends a completion request to the Ollama /api/chat endpoint.
func (p *OllamaProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	log.Debug().Str("model", req.Model).Msg("Ollama Complete called")

	var messages []ollamaMessage

	// Add system prompt as a system message
	if req.SystemPrompt != "" {
		messages = append(messages, ollamaMessage{
			Role:    string(RoleSystem),
			Content: req.SystemPrompt,
		})
	}

	// Add conversation messages
	for _, m := range req.Messages {
		messages = append(messages, ollamaMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	apiReq := ollamaChatRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   false, // non-streaming for simplicity
	}

	// Set options
	if req.Temperature > 0 || req.MaxTokens > 0 {
		apiReq.Options = &ollamaOptions{
			Temperature: req.Temperature,
			NumPredict:  req.MaxTokens,
		}
	}

	// Set structured output format if schema is provided
	if req.ResponseSchema != nil {
		apiReq.Format = req.ResponseSchema
	}

	reqBytes, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ollama request: %w", err)
	}

	url := p.endpoint + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	log.Debug().RawJSON("request", reqBytes).Msg("Sending request to Ollama")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request to Ollama failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Ollama response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	log.Debug().RawJSON("response", respBytes).Msg("Received response from Ollama")

	var apiResp ollamaChatResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ollama response: %w", err)
	}

	result := &CompletionResponse{
		Content: apiResp.Message.Content,
		Usage: TokenUsage{
			PromptTokens:     apiResp.PromptEvalCount,
			CompletionTokens: apiResp.EvalCount,
			TotalTokens:      apiResp.PromptEvalCount + apiResp.EvalCount,
		},
		Model:    apiResp.Model,
		Provider: p.Name(),
	}

	// If structured output was requested, parse the content as JSON
	if req.ResponseSchema != nil {
		// Validate that the content is valid JSON
		if json.Valid([]byte(result.Content)) {
			result.StructuredOutput = json.RawMessage(result.Content)
		} else {
			return nil, fmt.Errorf("ollama returned invalid JSON for structured output: %.200s", result.Content)
		}
	}

	return result, nil
}
