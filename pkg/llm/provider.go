// Package llm provides a model-agnostic abstraction layer for interacting
// with large language models. It defines the Provider interface implemented
// by Ollama, OpenAI, Anthropic, and Gemini backends.
package llm

import (
	"context"
	"encoding/json"
)

// Provider defines the interface for interacting with LLM backends.
// All providers must return structured JSON when a ResponseSchema is provided.
type Provider interface {
	// Name returns the provider's identifier (e.g., "ollama", "openai").
	Name() string

	// Complete sends a completion request and returns the response.
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}

// Role represents a message role in the conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message represents a single message in a conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// CompletionRequest contains the parameters for an LLM completion.
type CompletionRequest struct {
	// Model is the model identifier (e.g., "gpt-4o", "claude-3-5-sonnet-20241022").
	Model string `json:"model"`

	// SystemPrompt sets the system-level instructions.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// Messages is the conversation history.
	Messages []Message `json:"messages"`

	// ResponseSchema, when set, requests structured JSON output conforming
	// to this schema. The schema is a JSON Schema object.
	ResponseSchema json.RawMessage `json:"response_schema,omitempty"`

	// Temperature controls randomness (0.0 = deterministic, 1.0 = creative).
	Temperature float64 `json:"temperature"`

	// MaxTokens limits the response length.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// CompletionResponse contains the result of an LLM completion.
type CompletionResponse struct {
	// Content is the raw text response.
	Content string `json:"content"`

	// StructuredOutput contains the parsed JSON when ResponseSchema was provided.
	StructuredOutput json.RawMessage `json:"structured_output,omitempty"`

	// Usage tracks token consumption for cost estimation.
	Usage TokenUsage `json:"usage"`

	// Model is the model that was actually used (may differ from requested).
	Model string `json:"model"`

	// Provider is the name of the provider that handled the request.
	Provider string `json:"provider"`
}

// TokenUsage tracks token consumption.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
