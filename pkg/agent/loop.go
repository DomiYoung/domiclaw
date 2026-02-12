// Package agent provides the core agent loop for DomiClaw.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
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

	running  bool
	mu       sync.Mutex
	stopChan chan struct{}
}

// NewLoop creates a new agent loop.
func NewLoop(cfg *config.Config) (*Loop, error) {
	// Get API key
	apiKey := cfg.GetAnthropicAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set. Set it via environment variable or config file")
	}

	// Create provider
	var apiBase string
	if cfg.Providers.Anthropic != nil {
		apiBase = cfg.Providers.Anthropic.APIBase
	}
	provider := providers.NewAnthropicProvider(apiKey, apiBase)

	// Create tool registry
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(&tools.ReadFileTool{})
	toolRegistry.Register(&tools.WriteFileTool{Workspace: cfg.WorkspacePath()})
	toolRegistry.Register(&tools.ListDirTool{})
	toolRegistry.Register(tools.NewExecTool(cfg.WorkspacePath()))

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

		// Call LLM
		resp, err := l.provider.Chat(ctx, messages, toolDefs, l.cfg.Agents.Model, map[string]interface{}{
			"max_tokens":  l.cfg.Agents.MaxTokens,
			"temperature": l.cfg.Agents.Temperature,
		})

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
				fmt.Println(resp.Content)
			}
			return nil
		}

		// Print any content before tool calls
		if resp.Content != "" {
			fmt.Println(resp.Content)
		}

		// Build assistant message with tool calls
		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: resp.Content,
		}
		for _, tc := range resp.ToolCalls {
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:        tc.ID,
				Type:      "function",
				Name:      tc.Name,
				Arguments: tc.Arguments,
				Function:  tc.Function,
			})
		}
		messages = append(messages, assistantMsg)

		// Execute tool calls
		for _, tc := range resp.ToolCalls {
			logger.InfoCF("agent", fmt.Sprintf("Tool: %s", tc.Name), map[string]interface{}{
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
	basePrompt := `You are DomiClaw, an AI coding assistant. You help users with software engineering tasks.

You have access to tools for reading and writing files, listing directories, and executing commands.
Use these tools to help the user accomplish their tasks.

Be concise and helpful. Focus on completing the task efficiently.
`

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
