// Package providers provides LLM provider abstractions for DomiClaw.
package providers

import (
	"context"
)

// Message represents a chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool call from the LLM.
type ToolCall struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Name      string                 `json:"name"`
	Function  *FunctionCall          `json:"function,omitempty"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// FunctionCall represents a function call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDefinition defines a tool for the LLM.
type ToolDefinition struct {
	Type     string                 `json:"type"`
	Function ToolFunctionDefinition `json:"function"`
}

// ToolFunctionDefinition defines a function tool.
type ToolFunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Response represents a chat response from the LLM.
type Response struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     Usage      `json:"usage"`
}

// Usage represents token usage.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamEvent represents a streaming event from the LLM.
type StreamEvent struct {
	Type   string // "text", "tool_start", "tool_delta", "tool_end", "done", "error"
	Text   string // For "text" events
	ToolID string // For tool events
	Name   string // For "tool_start"
	Input  string // For "tool_delta" (partial JSON)
	Usage  *Usage // For "done" events
	Error  string // For "error" events
}

// StreamCallback is called for each streaming event.
type StreamCallback func(event StreamEvent)

// Provider is the interface for LLM providers.
type Provider interface {
	// Chat sends a chat request and returns the response.
	Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*Response, error)

	// ChatStream sends a chat request and streams the response via callback.
	// Returns the final aggregated response when complete.
	ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, callback StreamCallback) (*Response, error)

	// Name returns the provider name.
	Name() string
}
