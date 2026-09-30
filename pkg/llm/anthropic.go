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

// AnthropicProvider implements the Provider interface for Anthropic.
type AnthropicProvider struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

// NewAnthropicProvider creates a new AnthropicProvider.
func NewAnthropicProvider(endpoint, apiKey string) *AnthropicProvider {
	if endpoint == "" {
		endpoint = "https://api.anthropic.com/v1/messages"
	}
	return &AnthropicProvider{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   &http.Client{},
	}
}

// Name returns the provider's identifier.
func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
}

type anthropicResponse struct {
	Id      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Complete sends a completion request to Anthropic and returns the response.
func (p *AnthropicProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	log.Debug().Msgf("Anthropic Complete called with model: %s", req.Model)

	var messages []anthropicMessage
	for _, m := range req.Messages {
		role := string(m.Role)
		if role == string(RoleSystem) {
			// Anthropic doesn't allow system role in messages array
			continue
		}
		messages = append(messages, anthropicMessage{
			Role:    role,
			Content: m.Content,
		})
	}

	systemPrompt := req.SystemPrompt
	if req.ResponseSchema != nil {
		schemaMsg := fmt.Sprintf("\n\nYou must respond ONLY with valid JSON that conforms to this schema:\n%s", string(req.ResponseSchema))
		systemPrompt += schemaMsg
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096 // Anthropic requires max_tokens
	}

	apiReq := anthropicRequest{
		Model:       req.Model,
		Messages:    messages,
		System:      systemPrompt,
		Temperature: req.Temperature,
		MaxTokens:   maxTokens,
	}

	reqBytes, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal anthropic request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if p.apiKey != "" {
		httpReq.Header.Set("x-api-key", p.apiKey)
	}

	log.Debug().RawJSON("request", reqBytes).Msg("Sending request to Anthropic")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	log.Debug().RawJSON("response", respBytes).Msg("Received response from Anthropic")

	var apiResp anthropicResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal anthropic response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("no content returned in anthropic response")
	}

	textContent := ""
	for _, c := range apiResp.Content {
		if c.Type == "text" {
			textContent = c.Text
			break
		}
	}

	result := &CompletionResponse{
		Content: textContent,
		Usage: TokenUsage{
			PromptTokens:     apiResp.Usage.InputTokens,
			CompletionTokens: apiResp.Usage.OutputTokens,
			TotalTokens:      apiResp.Usage.InputTokens + apiResp.Usage.OutputTokens,
		},
		Model:    apiResp.Model,
		Provider: p.Name(),
	}

	if req.ResponseSchema != nil {
		result.StructuredOutput = json.RawMessage(result.Content)
	}

	return result, nil
}
