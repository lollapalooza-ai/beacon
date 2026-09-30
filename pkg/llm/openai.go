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

// OpenAIProvider implements the Provider interface for OpenAI.
type OpenAIProvider struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

// NewOpenAIProvider creates a new OpenAIProvider.
func NewOpenAIProvider(endpoint, apiKey string) *OpenAIProvider {
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/chat/completions"
	}
	return &OpenAIProvider{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   &http.Client{},
	}
}

// Name returns the provider's identifier.
func (p *OpenAIProvider) Name() string {
	return "openai"
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponseFormat struct {
	Type       string         `json:"type"`
	JSONSchema map[string]any `json:"json_schema,omitempty"`
}

type openAIRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	Temperature    float64               `json:"temperature,omitempty"`
	MaxTokens      int                   `json:"max_tokens,omitempty"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
}

type openAIResponse struct {
	Id      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Complete sends a completion request to OpenAI and returns the response.
func (p *OpenAIProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	log.Debug().Msgf("OpenAI Complete called with model: %s", req.Model)

	var messages []openAIMessage
	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{
			Role:    string(RoleSystem),
			Content: req.SystemPrompt,
		})
	}
	for _, m := range req.Messages {
		messages = append(messages, openAIMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	apiReq := openAIRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}

	if req.ResponseSchema != nil {
		var schemaMap map[string]any
		if err := json.Unmarshal(req.ResponseSchema, &schemaMap); err != nil {
			return nil, fmt.Errorf("failed to parse response schema: %w", err)
		}
		
		apiReq.ResponseFormat = &openAIResponseFormat{
			Type: "json_schema",
			JSONSchema: map[string]any{
				"name":   "response",
				"schema": schemaMap,
				"strict": true,
			},
		}
	}

	reqBytes, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	log.Debug().RawJSON("request", reqBytes).Msg("Sending request to OpenAI")

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
		return nil, fmt.Errorf("openai API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	log.Debug().RawJSON("response", respBytes).Msg("Received response from OpenAI")

	var apiResp openAIResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal openai response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned in openai response")
	}

	result := &CompletionResponse{
		Content: apiResp.Choices[0].Message.Content,
		Usage: TokenUsage{
			PromptTokens:     apiResp.Usage.PromptTokens,
			CompletionTokens: apiResp.Usage.CompletionTokens,
			TotalTokens:      apiResp.Usage.TotalTokens,
		},
		Model:    apiResp.Model,
		Provider: p.Name(),
	}

	if req.ResponseSchema != nil {
		result.StructuredOutput = json.RawMessage(result.Content)
	}

	return result, nil
}
