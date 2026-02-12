// Package tools provides tool implementations for DomiClaw.
package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Tool is the interface that all tools must implement.
type Tool interface {
	// Name returns the tool name.
	Name() string

	// Description returns the tool description.
	Description() string

	// Parameters returns the JSON schema for the tool parameters.
	Parameters() map[string]interface{}

	// Execute executes the tool with the given arguments.
	Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

// Registry manages available tools.
type Registry struct {
	tools   map[string]Tool
	aliases map[string]string // alias -> canonical name
	mu      sync.RWMutex
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:   make(map[string]Tool),
		aliases: make(map[string]string),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// RegisterAlias maps an alias name to a canonical tool name.
// This allows models that expect different tool names (e.g. "Bash" instead of "exec")
// to work seamlessly.
func (r *Registry) RegisterAlias(alias, canonical string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[strings.ToLower(alias)] = canonical
}

// resolveAlias resolves a tool name through aliases (case-insensitive).
func (r *Registry) resolveAlias(name string) string {
	if canonical, ok := r.aliases[strings.ToLower(name)]; ok {
		return canonical
	}
	return name
}

// ResolveName returns the canonical tool name for a given name (resolving aliases).
// This should be used when recording tool calls back into message history.
func (r *Registry) ResolveName(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolveAlias(name)
}

// Get retrieves a tool by name (with alias resolution).
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	resolved := r.resolveAlias(name)
	tool, ok := r.tools[resolved]
	return tool, ok
}

// List returns all registered tool names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// GetDefinitions returns tool definitions for the LLM.
func (r *Registry) GetDefinitions() []map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]map[string]interface{}, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        tool.Name(),
				"description": tool.Description(),
				"parameters":  tool.Parameters(),
			},
		})
	}
	return defs
}

// Execute runs a tool by name with the given arguments.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	r.mu.RLock()
	resolved := r.resolveAlias(name)
	tool, ok := r.tools[resolved]
	r.mu.RUnlock()

	if !ok {
		return "", &ToolNotFoundError{
			Name:           name,
			AvailableTools: r.List(),
		}
	}

	return tool.Execute(ctx, args)
}

// ToolNotFoundError is returned when a tool is not found.
type ToolNotFoundError struct {
	Name           string
	AvailableTools []string
}

func (e *ToolNotFoundError) Error() string {
	return fmt.Sprintf("tool not found: %s. Available tools: %s", e.Name, strings.Join(e.AvailableTools, ", "))
}
