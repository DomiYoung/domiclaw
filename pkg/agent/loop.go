// Package agent provides the core agent loop for DomiClaw.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/DomiYoung/domiclaw/pkg/config"
	"github.com/DomiYoung/domiclaw/pkg/logger"
	"github.com/DomiYoung/domiclaw/pkg/memory"
	"github.com/DomiYoung/domiclaw/pkg/providers"
	"github.com/DomiYoung/domiclaw/pkg/session"
	"github.com/DomiYoung/domiclaw/pkg/tools"
	"github.com/DomiYoung/domiclaw/pkg/utils"
)

// Loop manages the agent execution loop.
type Loop struct {
	cfg      *config.Config
	provider providers.Provider
	memory   *memory.Store
	sessions *session.Manager
	tools    *tools.Registry

	// For interactive mode: persistent message history
	messages []providers.Message
	toolDefs []providers.ToolDefinition

	running  bool
	mu       sync.Mutex
	stopChan chan struct{}
}

// NewLoop creates a new agent loop.
func NewLoop(cfg *config.Config) (*Loop, error) {
	// Create provider based on config
	provider, err := createProvider(cfg)
	if err != nil {
		return nil, err
	}

	// Determine working directory for command execution
	// Use current working directory (where user ran domiclaw), not the internal workspace
	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = cfg.WorkspacePath()
	}

	// Create tool registry with all available tools
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(&tools.ReadFileTool{})
	toolRegistry.Register(&tools.WriteFileTool{Workspace: workingDir})
	toolRegistry.Register(&tools.ListDirTool{})
	toolRegistry.Register(&tools.EditFileTool{Workspace: workingDir})
	toolRegistry.Register(&tools.GlobTool{Workspace: workingDir})
	toolRegistry.Register(&tools.GrepTool{Workspace: workingDir})
	toolRegistry.Register(tools.NewExecTool(workingDir))

	// Register web search if API key available
	if searchKey := cfg.GetSearchAPIKey(); searchKey != "" {
		toolRegistry.Register(tools.NewWebSearchTool(searchKey, cfg.Tools.Web.Search.MaxResults))
	}

	// Register aliases for Claude model compatibility
	// Claude models are trained with specific tool names from Claude Code
	toolRegistry.RegisterAlias("Bash", "exec")
	toolRegistry.RegisterAlias("bash", "exec")
	toolRegistry.RegisterAlias("Read", "read_file")
	toolRegistry.RegisterAlias("Write", "write_file")
	toolRegistry.RegisterAlias("Edit", "edit_file")
	toolRegistry.RegisterAlias("Glob", "glob")
	toolRegistry.RegisterAlias("Grep", "grep")
	toolRegistry.RegisterAlias("LS", "list_dir")
	toolRegistry.RegisterAlias("WebSearch", "web_search")

	return &Loop{
		cfg:      cfg,
		provider: provider,
		memory:   memory.NewStore(cfg.WorkspacePath()),
		sessions: session.NewManager(cfg.SessionsDir()),
		tools:    toolRegistry,
		stopChan: make(chan struct{}),
	}, nil
}

// Run starts the agent loop with the given prompt.
func (l *Loop) Run(ctx context.Context, initialPrompt string) error {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return fmt.Errorf("agent loop is already running")
	}
	l.running = true
	l.stopChan = make(chan struct{})
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.running = false
		l.mu.Unlock()
	}()

	// Check for pending resume
	if l.memory.HasPendingResume() {
		logger.Info("Found pending session to resume")
		resumePrompt := l.memory.ReadResumePrompt()
		if resumePrompt != "" {
			initialPrompt = resumePrompt
			l.memory.ClearResumeTrigger()
		}
	}

	return l.runAgentLoop(ctx, initialPrompt)
}

// Stop stops the agent loop.
func (l *Loop) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.running {
		return
	}

	close(l.stopChan)
}

// ClearHistory clears the conversation history for interactive mode.
func (l *Loop) ClearHistory() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = nil
}

// RunContinue continues an interactive conversation.
// Unlike Run(), it preserves message history across calls.
func (l *Loop) RunContinue(ctx context.Context, userPrompt string) error {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return fmt.Errorf("agent is already running")
	}
	l.running = true
	l.stopChan = make(chan struct{})
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.running = false
		l.mu.Unlock()
	}()

	// Initialize messages if this is the first call
	if len(l.messages) == 0 {
		l.messages = l.buildInitialMessages(userPrompt)
		l.toolDefs = l.buildToolDefinitions()
	} else {
		// Append user message to existing history
		l.messages = append(l.messages, providers.Message{
			Role:    "user",
			Content: userPrompt,
		})
	}

	return l.runInteractiveLoop(ctx)
}

// runInteractiveLoop runs the agent loop using persistent messages.
func (l *Loop) runInteractiveLoop(ctx context.Context) error {
	var lastToolSig string
	repeatCount := 0
	const maxRepeats = 2

	for iteration := 0; iteration < l.cfg.Agents.MaxToolIterations; iteration++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.stopChan:
			return nil
		default:
		}

		// Call LLM with streaming (with retry for rate limits)
		var resp *providers.Response
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			resp, err = l.provider.ChatStream(ctx, l.messages, l.toolDefs, l.cfg.Agents.Model, map[string]interface{}{
				"max_tokens":  l.cfg.Agents.MaxTokens,
				"temperature": l.cfg.Agents.Temperature,
			}, func(event providers.StreamEvent) {
				switch event.Type {
				case "text":
					fmt.Print(event.Text)
				case "tool_start":
					fmt.Printf("\n[tool: %s] ", event.Name)
				}
			})
			if err == nil {
				break
			}
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "rate_limit") || strings.Contains(errStr, "too many") || strings.Contains(errStr, "429") {
				backoff := time.Duration(5*(attempt+1)) * time.Second
				logger.WarnCF("agent", "Rate limited, retrying", map[string]interface{}{
					"attempt": attempt + 1,
					"backoff": backoff.String(),
				})
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-l.stopChan:
					return nil
				case <-time.After(backoff):
					continue
				}
			}
			break
		}

		if err != nil {
			if l.detectContextOverflow(err) {
				return l.handleContextOverflow()
			}
			return fmt.Errorf("LLM call failed: %w", err)
		}

		logger.InfoCF("agent", "LLM response", map[string]interface{}{
			"tokens_in":   resp.Usage.PromptTokens,
			"tokens_out":  resp.Usage.CompletionTokens,
			"tool_calls":  len(resp.ToolCalls),
			"has_content": resp.Content != "",
		})

		// If no tool calls, conversation turn is complete
		if len(resp.ToolCalls) == 0 {
			if resp.Content != "" {
				// Store assistant response in history
				l.messages = append(l.messages, providers.Message{
					Role:    "assistant",
					Content: resp.Content,
				})
			}
			return nil
		}

		// Build assistant message with tool calls
		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: resp.Content,
		}
		for _, tc := range resp.ToolCalls {
			resolvedName := l.tools.ResolveName(tc.Name)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:        tc.ID,
				Type:      "function",
				Name:      resolvedName,
				Arguments: tc.Arguments,
				Function:  tc.Function,
			})
		}
		l.messages = append(l.messages, assistantMsg)

		// Execute tool calls
		for _, tc := range resp.ToolCalls {
			resolvedName := l.tools.ResolveName(tc.Name)
			logger.InfoCF("agent", fmt.Sprintf("Tool: %s", resolvedName), map[string]interface{}{
				"args": utils.Truncate(fmt.Sprintf("%v", tc.Arguments), 100),
			})

			result, err := l.tools.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
				logger.WarnCF("agent", "Tool execution failed", map[string]interface{}{
					"tool":  resolvedName,
					"error": err.Error(),
				})
			}

			displayResult := utils.Truncate(result, 200)
			fmt.Printf("  → %s\n", displayResult)

			l.messages = append(l.messages, providers.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}

		// Detect repeated identical tool calls
		currentSig := marshalArgs(resp.ToolCalls[0].Arguments) + ":" + resp.ToolCalls[0].Name
		if currentSig == lastToolSig {
			repeatCount++
			if repeatCount >= maxRepeats {
				logger.WarnCF("agent", "Breaking tool call loop", map[string]interface{}{
					"tool":    resp.ToolCalls[0].Name,
					"repeats": repeatCount + 1,
				})
				l.messages = append(l.messages, providers.Message{
					Role:    "user",
					Content: "You are repeating the same tool call. Please use the results you already have and provide your final answer.",
				})
				lastToolSig = ""
				repeatCount = 0
			}
		} else {
			lastToolSig = currentSig
			repeatCount = 0
		}
	}

	logger.WarnCF("agent", "Max iterations reached", map[string]interface{}{
		"max": l.cfg.Agents.MaxToolIterations,
	})

	return nil
}

// runAgentLoop executes the main agent loop.
func (l *Loop) runAgentLoop(ctx context.Context, userPrompt string) error {
	logger.InfoCF("agent", "Starting agent loop", map[string]interface{}{
		"model":     l.cfg.Agents.Model,
		"workspace": l.cfg.WorkspacePath(),
	})

	// Build initial messages
	messages := l.buildInitialMessages(userPrompt)

	// Get tool definitions
	toolDefs := l.buildToolDefinitions()

	// Main loop
	var lastToolSig string
	repeatCount := 0
	const maxRepeats = 2

	for iteration := 0; iteration < l.cfg.Agents.MaxToolIterations; iteration++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.stopChan:
			return nil
		default:
		}

		logger.DebugCF("agent", "LLM iteration", map[string]interface{}{
			"iteration": iteration + 1,
			"max":       l.cfg.Agents.MaxToolIterations,
		})

		// Call LLM with streaming (with retry for rate limits)
		var resp *providers.Response
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			resp, err = l.provider.ChatStream(ctx, messages, toolDefs, l.cfg.Agents.Model, map[string]interface{}{
				"max_tokens":  l.cfg.Agents.MaxTokens,
				"temperature": l.cfg.Agents.Temperature,
			}, func(event providers.StreamEvent) {
				switch event.Type {
				case "text":
					fmt.Print(event.Text)
				case "tool_start":
					fmt.Printf("\n[tool: %s] ", event.Name)
				case "done":
					// Print newline after streamed text
				}
			})
			if err == nil {
				break
			}
			// Retry on rate limit errors
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "rate_limit") || strings.Contains(errStr, "too many") || strings.Contains(errStr, "429") {
				backoff := time.Duration(5*(attempt+1)) * time.Second
				logger.WarnCF("agent", "Rate limited, retrying", map[string]interface{}{
					"attempt": attempt + 1,
					"backoff": backoff.String(),
				})
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-l.stopChan:
					return nil
				case <-time.After(backoff):
					continue
				}
			}
			break // Non-retryable error
		}

		if err != nil {
			// Check for context overflow
			if l.detectContextOverflow(err) {
				return l.handleContextOverflow()
			}
			return fmt.Errorf("LLM call failed: %w", err)
		}

		// Log usage
		logger.InfoCF("agent", "LLM response", map[string]interface{}{
			"tokens_in":   resp.Usage.PromptTokens,
			"tokens_out":  resp.Usage.CompletionTokens,
			"tool_calls":  len(resp.ToolCalls),
			"has_content": resp.Content != "",
		})

		// If no tool calls, we're done
		if len(resp.ToolCalls) == 0 {
			if resp.Content != "" {
				fmt.Println() // newline after streamed text
			}
			return nil
		}

		// Print newline after streamed content before tool execution output
		if resp.Content != "" {
			fmt.Println()
		}

		// Build assistant message with tool calls (use resolved canonical names)
		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: resp.Content,
		}
		for _, tc := range resp.ToolCalls {
			resolvedName := l.tools.ResolveName(tc.Name)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:        tc.ID,
				Type:      "function",
				Name:      resolvedName,
				Arguments: tc.Arguments,
				Function:  tc.Function,
			})
		}
		messages = append(messages, assistantMsg)

		// Execute tool calls
		for _, tc := range resp.ToolCalls {
			resolvedName := l.tools.ResolveName(tc.Name)
			logger.InfoCF("agent", fmt.Sprintf("Tool: %s", resolvedName), map[string]interface{}{
				"args": utils.Truncate(fmt.Sprintf("%v", tc.Arguments), 100),
			})

			result, err := l.tools.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
				logger.WarnCF("agent", "Tool execution failed", map[string]interface{}{
					"tool":  tc.Name,
					"error": err.Error(),
				})
			}

			// Truncate long results for display
			displayResult := utils.Truncate(result, 200)
			fmt.Printf("  → %s\n", displayResult)

			// Add tool result to messages
			messages = append(messages, providers.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}

		// Detect repeated identical tool calls to break infinite loops
		currentSig := marshalArgs(resp.ToolCalls[0].Arguments) + ":" + resp.ToolCalls[0].Name
		if currentSig == lastToolSig {
			repeatCount++
			if repeatCount >= maxRepeats {
				logger.WarnCF("agent", "Breaking tool call loop - same call repeated", map[string]interface{}{
					"tool":    resp.ToolCalls[0].Name,
					"repeats": repeatCount + 1,
				})
				// Add a hint to the model to stop repeating
				messages = append(messages, providers.Message{
					Role:    "user",
					Content: "You are repeating the same tool call. Please use the results you already have and provide your final answer. Do not make any more tool calls.",
				})
				lastToolSig = ""
				repeatCount = 0
			}
		} else {
			lastToolSig = currentSig
			repeatCount = 0
		}

		// Check for strategic compact boundary
		if l.cfg.StrategicCompact.Enabled && resp.Content != "" {
			l.checkStrategicBoundary(resp.Content)
		}
	}

	logger.WarnCF("agent", "Max iterations reached", map[string]interface{}{
		"max": l.cfg.Agents.MaxToolIterations,
	})

	return nil
}

// buildInitialMessages creates the initial message list.
func (l *Loop) buildInitialMessages(userPrompt string) []providers.Message {
	var messages []providers.Message

	// System prompt with memory context
	systemPrompt := l.buildSystemPrompt()
	messages = append(messages, providers.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	// User message
	messages = append(messages, providers.Message{
		Role:    "user",
		Content: userPrompt,
	})

	return messages
}

// buildSystemPrompt creates the system prompt with memory context.
func (l *Loop) buildSystemPrompt() string {
	// List available tool names
	toolNames := strings.Join(l.tools.List(), ", ")

	basePrompt := fmt.Sprintf(`You are DomiClaw, an AI coding assistant. You help users with software engineering tasks.

You have the following tools available: %s

Tool usage:
- Use "exec" to run shell commands (bash/sh). The argument is "command" (string).
- Use "read_file" to read file contents. The argument is "path" (string).
- Use "write_file" to create/overwrite files. Arguments: "path" and "content".
- Use "edit_file" to make targeted edits. Arguments: "path", "old_string", "new_string".
- Use "list_dir" to list directory contents. The argument is "path" (string).
- Use "glob" to find files by pattern. The argument is "pattern" (string).
- Use "grep" to search file contents. Arguments: "pattern" and optionally "path", "include".
- Use "web_search" to search the web. The argument is "query" (string).

IMPORTANT: Only use the tool names listed above. Do NOT use tool names like "Bash", "Read", "Write", etc.

Be concise and helpful. Focus on completing the task efficiently.
`, toolNames)

	// Add memory context
	memoryCtx := l.memory.GetMemoryContext(l.cfg.Memory.DailyNotesDays)
	if memoryCtx != "" {
		basePrompt += "\n---\n\n" + memoryCtx
	}

	return basePrompt
}

// buildToolDefinitions creates tool definitions for the LLM.
func (l *Loop) buildToolDefinitions() []providers.ToolDefinition {
	defs := l.tools.GetDefinitions()
	var result []providers.ToolDefinition

	for _, def := range defs {
		fn := def["function"].(map[string]interface{})
		result = append(result, providers.ToolDefinition{
			Type: "function",
			Function: providers.ToolFunctionDefinition{
				Name:        fn["name"].(string),
				Description: fn["description"].(string),
				Parameters:  fn["parameters"].(map[string]interface{}),
			},
		})
	}

	return result
}

// checkStrategicBoundary checks for strategic compact boundary patterns.
func (l *Loop) checkStrategicBoundary(content string) {
	for _, pattern := range l.cfg.StrategicCompact.BoundaryPatterns {
		if strings.Contains(content, pattern) {
			logger.InfoCF("agent", "Strategic boundary detected", map[string]interface{}{
				"pattern": pattern,
			})
			l.memory.AppendToday(fmt.Sprintf("## Strategic Boundary: %s\n\nDetected at %s\n",
				pattern, time.Now().Format("15:04:05")))
			return
		}
	}
}

// detectContextOverflow checks if an error indicates context overflow.
func (l *Loop) detectContextOverflow(err error) bool {
	errStr := strings.ToLower(err.Error())
	patterns := []string{
		"context_length_exceeded",
		"maximum context length",
		"token limit",
		"too many tokens",
	}

	for _, pattern := range patterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// handleContextOverflow handles context overflow by creating recovery files.
func (l *Loop) handleContextOverflow() error {
	logger.WarnCF("agent", "Context overflow detected, initiating recovery", nil)

	sessionID := fmt.Sprintf("session_%d", time.Now().Unix())

	// Write resume trigger
	l.memory.WriteResumeTrigger(sessionID, "context_overflow")

	// Generate gap analysis prompt
	gapPrompt := l.generateGapAnalysisPrompt()
	l.memory.WriteResumePrompt(gapPrompt)

	// Log to daily notes
	l.memory.AppendToday(fmt.Sprintf(`## Context Overflow Recovery

Time: %s
Session: %s

Context overflow detected. Resume trigger created.
Run 'domiclaw resume' to continue.
`, time.Now().Format("15:04:05"), sessionID))

	return fmt.Errorf("context overflow - run 'domiclaw resume' to continue")
}

// generateGapAnalysisPrompt creates the prompt for gap analysis recovery.
func (l *Loop) generateGapAnalysisPrompt() string {
	memoryCtx := l.memory.GetMemoryContext(l.cfg.Memory.DailyNotesDays)

	return fmt.Sprintf(`# Session Recovery - Gap Analysis

You are resuming from a context overflow. Before continuing:

1. **Review Memory Context** below
2. **Identify Knowledge Gaps** - What information might be missing?
3. **Read Relevant Files** - Use file tools to recover context
4. **Continue the Task** - Resume where you left off

## Important
- Do NOT make assumptions about previous work
- Verify file states before making changes
- Check git status if applicable

---

%s

---

Please perform gap analysis and then continue the task.
`, memoryCtx)
}

// GetTools returns the tool registry for external access.
func (l *Loop) GetTools() *tools.Registry {
	return l.tools
}

// Helper for JSON marshaling tool call arguments
func marshalArgs(args map[string]interface{}) string {
	data, _ := json.Marshal(args)
	return string(data)
}

// createProvider creates the appropriate LLM provider based on config.
// Priority: 1. Anthropic (with optional custom proxy), 2. Honoursoft (OpenAI-compatible), 3. OpenRouter
func createProvider(cfg *config.Config) (providers.Provider, error) {
	// Try Anthropic first (supports like-ai.cc proxy via ANTHROPIC_BASE_URL)
	if apiKey := cfg.GetAnthropicAPIKey(); apiKey != "" {
		apiBase := cfg.GetAnthropicAPIBase()
		if apiBase != "" {
			logger.InfoCF("provider", "Using Anthropic provider (custom proxy)", map[string]interface{}{
				"base_url": apiBase,
			})
		} else {
			logger.Info("Using Anthropic provider (direct)")
		}
		return providers.NewAnthropicProvider(apiKey, apiBase), nil
	}

	// Try Honoursoft (OpenAI-compatible proxy)
	if apiKey := cfg.GetHonoursoftAPIKey(); apiKey != "" {
		apiBase := cfg.GetHonoursoftAPIBase()
		if apiBase == "" {
			return nil, fmt.Errorf("HONOURSOFT_API_KEY set but no base URL. Set HONOURSOFT_BASE_URL")
		}
		logger.InfoCF("provider", "Using Honoursoft provider", map[string]interface{}{
			"base_url": apiBase,
		})
		return providers.NewOpenAICompatibleProvider("honoursoft", apiKey, apiBase), nil
	}

	// Try OpenRouter
	if apiKey := cfg.GetOpenRouterAPIKey(); apiKey != "" {
		logger.Info("Using OpenRouter provider")
		return providers.NewOpenRouterProvider(apiKey), nil
	}

	return nil, fmt.Errorf("no API key configured. Set ANTHROPIC_API_KEY, HONOURSOFT_API_KEY, or OPENROUTER_API_KEY")
}
