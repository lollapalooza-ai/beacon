package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"
)

// GeminiProvider implements the Provider interface for Google Gemini.
type GeminiProvider struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

// NewGeminiProvider creates a new GeminiProvider.
func NewGeminiProvider(endpoint, apiKey string) *GeminiProvider {
	if endpoint == "" {
		endpoint = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"
	}
	return &GeminiProvider{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   &http.Client{},
	}
}

// Name returns the provider's identifier.
func (p *GeminiProvider) Name() string {
	return "gemini"
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	Temperature      float64         `json:"temperature,omitempty"`
	MaxOutputTokens  int             `json:"maxOutputTokens,omitempty"`
	ResponseMimeType string          `json:"responseMimeType,omitempty"`
	ResponseSchema   json.RawMessage `json:"responseSchema,omitempty"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	Contents          []geminiContent         `json:"contents"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

// Complete sends a completion request to Gemini and returns the response.
func (p *GeminiProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	log.Debug().Msgf("Gemini Complete called with model: %s", req.Model)

	var contents []geminiContent
	for _, m := range req.Messages {
		role := string(m.Role)
		if role == "assistant" {
			role = "model"
		}
		if role == "system" {
			continue // handled separately
		}
		contents = append(contents, geminiContent{
			Role: role,
			Parts: []geminiPart{
				{Text: m.Content},
			},
		})
	}

	apiReq := geminiRequest{
		Contents: contents,
	}

	if req.SystemPrompt != "" {
		apiReq.SystemInstruction = &geminiContent{
			Role: "user",
			Parts: []geminiPart{
				{Text: req.SystemPrompt},
			},
		}
	}

	genConfig := &geminiGenerationConfig{
		Temperature:     req.Temperature,
		MaxOutputTokens: req.MaxTokens,
	}

	if req.ResponseSchema != nil {
		genConfig.ResponseMimeType = "application/json"
		genConfig.ResponseSchema = req.ResponseSchema
	}

	apiReq.GenerationConfig = genConfig

	reqBytes, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gemini request: %w", err)
	}

	modelStr := req.Model
	if !strings.HasPrefix(modelStr, "models/") {
		modelStr = "models/" + modelStr
	}

	u := p.endpoint
	if strings.Contains(u, "%s") {
		u = fmt.Sprintf(u, strings.TrimPrefix(modelStr, "models/"))
	}

	parsedUrl, err := url.Parse(u)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint url: %w", err)
	}

	if p.apiKey != "" {
		q := parsedUrl.Query()
		q.Set("key", p.apiKey)
		parsedUrl.RawQuery = q.Encode()
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedUrl.String(), bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	log.Debug().RawJSON("request", reqBytes).Msg("Sending request to Gemini")

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
		return nil, fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	log.Debug().RawJSON("response", respBytes).Msg("Received response from Gemini")

	var apiResp geminiResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gemini response: %w", err)
	}

	if len(apiResp.Candidates) == 0 || len(apiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("no content returned in gemini response")
	}

	content := apiResp.Candidates[0].Content.Parts[0].Text

	result := &CompletionResponse{
		Content: content,
		Usage: TokenUsage{
			PromptTokens:     apiResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: apiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      apiResp.UsageMetadata.TotalTokenCount,
		},
		Model:    req.Model, // Use requested model as it might not be returned
		Provider: p.Name(),
	}

	if apiResp.ModelVersion != "" {
		result.Model = apiResp.ModelVersion
	}

	if req.ResponseSchema != nil {
		result.StructuredOutput = json.RawMessage(result.Content)
	}

	return result, nil
}
