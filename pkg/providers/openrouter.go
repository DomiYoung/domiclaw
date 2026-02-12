// Package providers provides OpenRouter LLM provider.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const openRouterAPIURL = "https://openrouter.ai/api/v1/chat/completions"

// OpenRouterProvider implements the Provider interface for OpenRouter.
type OpenRouterProvider struct {
	apiKey string
	client *http.Client
}

// NewOpenRouterProvider creates a new OpenRouter provider.
func NewOpenRouterProvider(apiKey string) *OpenRouterProvider {
	return &OpenRouterProvider{
		apiKey: apiKey,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Name returns the provider name.
func (p *OpenRouterProvider) Name() string {
	return "openrouter"
}

// OpenRouter uses OpenAI-compatible API format
type openRouterRequest struct {
	Model       string              `json:"model"`
	Messages    []openRouterMessage `json:"messages"`
	Tools       []openRouterTool    `json:"tools,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
}

type openRouterMessage struct {
	Role       string               `json:"role"`
	Content    interface{}          `json:"content"` // string or array
	ToolCalls  []openRouterToolCall `json:"tool_calls,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
}

type openRouterToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openRouterTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Parameters  map[string]interface{} `json:"parameters"`
	} `json:"function"`
}

type openRouterResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Role      string               `json:"role"`
			Content   string               `json:"content"`
			ToolCalls []openRouterToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Chat sends a chat request to OpenRouter.
func (p *OpenRouterProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*Response, error) {
	// Convert messages to OpenRouter format
	var orMessages []openRouterMessage

	for _, msg := range messages {
		orMsg := openRouterMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}

		// Handle tool call ID for tool results
		if msg.ToolCallID != "" {
			orMsg.ToolCallID = msg.ToolCallID
		}

		// Handle tool calls from assistant
		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				args := ""
				if tc.Function != nil {
					args = tc.Function.Arguments
				} else if tc.Arguments != nil {
					argsBytes, _ := json.Marshal(tc.Arguments)
					args = string(argsBytes)
				}
				orMsg.ToolCalls = append(orMsg.ToolCalls, openRouterToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Name,
						Arguments: args,
					},
				})
			}
		}

		orMessages = append(orMessages, orMsg)
	}

	// Convert tools to OpenRouter format
	var orTools []openRouterTool
	for _, tool := range tools {
		orTool := openRouterTool{
			Type: "function",
		}
		orTool.Function.Name = tool.Function.Name
		orTool.Function.Description = tool.Function.Description
		orTool.Function.Parameters = tool.Function.Parameters
		orTools = append(orTools, orTool)
	}

	// Build request
	maxTokens := 8192
	if v, ok := options["max_tokens"].(int); ok {
		maxTokens = v
	}

	temperature := 0.7
	if v, ok := options["temperature"].(float64); ok {
		temperature = v
	}

	reqBody := openRouterRequest{
		Model:       model,
		Messages:    orMessages,
		Tools:       orTools,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}

	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", openRouterAPIURL, bytes.NewReader(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/DomiYoung/domiclaw")
	req.Header.Set("X-Title", "DomiClaw")

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var orResp openRouterResponse
	if err := json.Unmarshal(respData, &orResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for errors
	if orResp.Error != nil {
		return nil, fmt.Errorf("openrouter API error: %s - %s", orResp.Error.Type, orResp.Error.Message)
	}

	if len(orResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	// Convert to our response format
	choice := orResp.Choices[0]
	result := &Response{
		Content: choice.Message.Content,
		Usage: Usage{
			PromptTokens:     orResp.Usage.PromptTokens,
			CompletionTokens: orResp.Usage.CompletionTokens,
			TotalTokens:      orResp.Usage.TotalTokens,
		},
	}

	// Convert tool calls
	for _, tc := range choice.Message.ToolCalls {
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args)

		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        tc.ID,
			Type:      "function",
			Name:      tc.Function.Name,
			Arguments: args,
			Function: &FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	return result, nil
}
