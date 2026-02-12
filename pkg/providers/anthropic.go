// Package providers provides LLM provider implementations.
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	anthropicDefaultBaseURL = "https://api.anthropic.com"
	anthropicMessagesPath   = "/v1/messages"
	anthropicAPIVersion     = "2023-06-01"
)

// AnthropicProvider implements the Provider interface for Anthropic.
type AnthropicProvider struct {
	apiKey     string
	apiBaseURL string // Full URL to the messages endpoint
	client     *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider.
// apiBase should be the base URL (e.g. "https://api.like-ai.cc") without path.
// If empty, defaults to the official Anthropic API.
func NewAnthropicProvider(apiKey, apiBase string) *AnthropicProvider {
	baseURL := anthropicDefaultBaseURL
	if apiBase != "" {
		baseURL = strings.TrimRight(apiBase, "/")
	}

	// Build full endpoint URL
	endpointURL := baseURL + anthropicMessagesPath

	return &AnthropicProvider{
		apiKey:     apiKey,
		apiBaseURL: endpointURL,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Name returns the provider name.
func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// anthropicRequest represents the request body for Anthropic API.
type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []contentBlock
}

type contentBlock struct {
	Type      string      `json:"type"`
	Text      string      `json:"text,omitempty"`
	ID        string      `json:"id,omitempty"`
	Name      string      `json:"name,omitempty"`
	Input     interface{} `json:"input,omitempty"`
	ToolUseID string      `json:"tool_use_id,omitempty"`
	Content   string      `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// anthropicResponse represents the response from Anthropic API.
type anthropicResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []contentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"`
	StopSequence string         `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Chat sends a chat request to Anthropic.
func (p *AnthropicProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*Response, error) {
	// Convert messages to Anthropic format
	var systemPrompt string
	var anthropicMsgs []anthropicMessage

	for _, msg := range messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
			continue
		}

		// Handle tool results
		if msg.Role == "tool" {
			// Tool results need to be part of a user message with tool_result content blocks
			anthropicMsgs = append(anthropicMsgs, anthropicMessage{
				Role: "user",
				Content: []contentBlock{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   msg.Content,
				}},
			})
			continue
		}

		// Handle assistant messages with tool calls
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			var blocks []contentBlock
			if msg.Content != "" {
				blocks = append(blocks, contentBlock{
					Type: "text",
					Text: msg.Content,
				})
			}
			for _, tc := range msg.ToolCalls {
				var input interface{}
				if tc.Function != nil {
					json.Unmarshal([]byte(tc.Function.Arguments), &input)
				} else {
					input = tc.Arguments
				}
				blocks = append(blocks, contentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
			anthropicMsgs = append(anthropicMsgs, anthropicMessage{
				Role:    "assistant",
				Content: blocks,
			})
			continue
		}

		// Regular message
		anthropicMsgs = append(anthropicMsgs, anthropicMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Convert tools to Anthropic format
	var anthropicTools []anthropicTool
	for _, tool := range tools {
		anthropicTools = append(anthropicTools, anthropicTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		})
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

	reqBody := anthropicRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Messages:    anthropicMsgs,
		System:      systemPrompt,
		Tools:       anthropicTools,
		Temperature: temperature,
	}

	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", p.apiBaseURL, bytes.NewReader(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

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

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		var errResp anthropicError
		if err := json.Unmarshal(respData, &errResp); err == nil {
			return nil, fmt.Errorf("anthropic API error: %s - %s", errResp.Error.Type, errResp.Error.Message)
		}
		return nil, fmt.Errorf("anthropic API error: status %d - %s", resp.StatusCode, string(respData))
	}

	// Parse response
	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respData, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to our response format
	result := &Response{
		Usage: Usage{
			PromptTokens:     anthropicResp.Usage.InputTokens,
			CompletionTokens: anthropicResp.Usage.OutputTokens,
			TotalTokens:      anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
		},
	}

	// Extract content and tool calls
	for _, block := range anthropicResp.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "tool_use":
			inputJSON, _ := json.Marshal(block.Input)
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:        block.ID,
				Type:      "function",
				Name:      block.Name,
				Arguments: block.Input.(map[string]interface{}),
				Function: &FunctionCall{
					Name:      block.Name,
					Arguments: string(inputJSON),
				},
			})
		}
	}

	return result, nil
}

// ChatStream sends a streaming chat request to Anthropic.
func (p *AnthropicProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, callback StreamCallback) (*Response, error) {
	// Convert messages (same as Chat)
	var systemPrompt string
	var anthropicMsgs []anthropicMessage

	for _, msg := range messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
			continue
		}
		if msg.Role == "tool" {
			anthropicMsgs = append(anthropicMsgs, anthropicMessage{
				Role: "user",
				Content: []contentBlock{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   msg.Content,
				}},
			})
			continue
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			var blocks []contentBlock
			if msg.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				var input interface{}
				if tc.Function != nil {
					json.Unmarshal([]byte(tc.Function.Arguments), &input)
				} else {
					input = tc.Arguments
				}
				blocks = append(blocks, contentBlock{
					Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: input,
				})
			}
			anthropicMsgs = append(anthropicMsgs, anthropicMessage{Role: "assistant", Content: blocks})
			continue
		}
		anthropicMsgs = append(anthropicMsgs, anthropicMessage{Role: msg.Role, Content: msg.Content})
	}

	// Convert tools
	var anthropicTools []anthropicTool
	for _, tool := range tools {
		anthropicTools = append(anthropicTools, anthropicTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		})
	}

	maxTokens := 8192
	if v, ok := options["max_tokens"].(int); ok {
		maxTokens = v
	}
	temperature := 0.7
	if v, ok := options["temperature"].(float64); ok {
		temperature = v
	}

	// Build request with stream: true
	reqBody := struct {
		anthropicRequest
		Stream bool `json:"stream"`
	}{
		anthropicRequest: anthropicRequest{
			Model:       model,
			MaxTokens:   maxTokens,
			Messages:    anthropicMsgs,
			System:      systemPrompt,
			Tools:       anthropicTools,
			Temperature: temperature,
		},
		Stream: true,
	}

	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.apiBaseURL, bytes.NewReader(reqData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check for non-200 (non-streaming error)
	if resp.StatusCode != http.StatusOK {
		respData, _ := io.ReadAll(resp.Body)
		var errResp anthropicError
		if err := json.Unmarshal(respData, &errResp); err == nil {
			return nil, fmt.Errorf("anthropic API error: %s - %s", errResp.Error.Type, errResp.Error.Message)
		}
		return nil, fmt.Errorf("anthropic API error: status %d - %s", resp.StatusCode, string(respData))
	}

	// Parse SSE stream
	return p.parseSSEStream(resp.Body, callback)
}

// SSE event types from Anthropic streaming API
type sseContentBlockStart struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type  string          `json:"type"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
		Text  string          `json:"text,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content_block"`
}

type sseContentBlockDelta struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

type sseMessageDelta struct {
	Type  string `json:"type"`
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type sseMessageStart struct {
	Type    string `json:"type"`
	Message struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func (p *AnthropicProvider) parseSSEStream(body io.Reader, callback StreamCallback) (*Response, error) {
	scanner := bufio.NewScanner(body)
	// Increase buffer for large SSE events
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	result := &Response{}

	var currentEventType string
	// Track tool calls by index
	type toolCallAccum struct {
		id        string
		name      string
		inputJSON strings.Builder
	}
	toolCalls := make(map[int]*toolCallAccum)

	for scanner.Scan() {
		line := scanner.Text()

		// SSE event type line
		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		// SSE data line
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		switch currentEventType {
		case "message_start":
			var evt sseMessageStart
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				result.Usage.PromptTokens = evt.Message.Usage.InputTokens
			}

		case "content_block_start":
			var evt sseContentBlockStart
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				if evt.ContentBlock.Type == "tool_use" {
					toolCalls[evt.Index] = &toolCallAccum{
						id:   evt.ContentBlock.ID,
						name: evt.ContentBlock.Name,
					}
					if callback != nil {
						callback(StreamEvent{
							Type:   "tool_start",
							ToolID: evt.ContentBlock.ID,
							Name:   evt.ContentBlock.Name,
						})
					}
				}
			}

		case "content_block_delta":
			var evt sseContentBlockDelta
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				switch evt.Delta.Type {
				case "text_delta":
					result.Content += evt.Delta.Text
					if callback != nil {
						callback(StreamEvent{Type: "text", Text: evt.Delta.Text})
					}
				case "input_json_delta":
					if tc, ok := toolCalls[evt.Index]; ok {
						tc.inputJSON.WriteString(evt.Delta.PartialJSON)
					}
					if callback != nil {
						callback(StreamEvent{
							Type:  "tool_delta",
							Input: evt.Delta.PartialJSON,
						})
					}
				}
			}

		case "content_block_stop":
			// Finalize any tool call at this index
			var evt struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				if tc, ok := toolCalls[evt.Index]; ok {
					var args map[string]interface{}
					inputStr := tc.inputJSON.String()
					json.Unmarshal([]byte(inputStr), &args)

					result.ToolCalls = append(result.ToolCalls, ToolCall{
						ID:        tc.id,
						Type:      "function",
						Name:      tc.name,
						Arguments: args,
						Function: &FunctionCall{
							Name:      tc.name,
							Arguments: inputStr,
						},
					})

					if callback != nil {
						callback(StreamEvent{
							Type:   "tool_end",
							ToolID: tc.id,
							Name:   tc.name,
						})
					}
				}
			}

		case "message_delta":
			var evt sseMessageDelta
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				result.Usage.CompletionTokens = evt.Usage.OutputTokens
				result.Usage.TotalTokens = result.Usage.PromptTokens + result.Usage.CompletionTokens
			}

		case "message_stop":
			if callback != nil {
				callback(StreamEvent{
					Type:  "done",
					Usage: &result.Usage,
				})
			}

		case "error":
			var evt struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				errMsg := fmt.Sprintf("%s: %s", evt.Error.Type, evt.Error.Message)
				if callback != nil {
					callback(StreamEvent{Type: "error", Error: errMsg})
				}
				return nil, fmt.Errorf("anthropic stream error: %s", errMsg)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE stream read error: %w", err)
	}

	return result, nil
}
