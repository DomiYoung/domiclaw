// Package config provides configuration management for DomiClaw.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/DomiYoung/domiclaw/pkg/utils"
)

// Config represents the DomiClaw configuration.
type Config struct {
	Workspace        string          `json:"workspace"`
	Agents           AgentsConfig    `json:"agents"`
	Providers        ProvidersConfig `json:"providers"`
	Tools            ToolsConfig     `json:"tools"`
	Memory           MemoryConfig    `json:"memory"`
	Heartbeat        HeartbeatConfig `json:"heartbeat"`
	StrategicCompact CompactConfig   `json:"strategic_compact"`
}

// AgentsConfig configures agent behavior.
type AgentsConfig struct {
	Model             string  `json:"model"`
	MaxTokens         int     `json:"max_tokens"`
	Temperature       float64 `json:"temperature"`
	MaxToolIterations int     `json:"max_tool_iterations"`
}

// ProvidersConfig configures LLM providers.
type ProvidersConfig struct {
	Anthropic  *ProviderConfig `json:"anthropic,omitempty"`
	OpenRouter *ProviderConfig `json:"openrouter,omitempty"`
}

// ProviderConfig represents a single provider's configuration.
type ProviderConfig struct {
	APIKey  string `json:"api_key,omitempty"`  // Optional: prefer env vars
	APIBase string `json:"api_base,omitempty"` // Optional: custom endpoint
}

// ToolsConfig configures built-in tools.
type ToolsConfig struct {
	Web WebToolsConfig `json:"web"`
}

// WebToolsConfig configures web-related tools.
type WebToolsConfig struct {
	Search SearchConfig `json:"search"`
}

// SearchConfig configures web search.
type SearchConfig struct {
	APIKey     string `json:"api_key,omitempty"` // Brave/Tavily API key
	MaxResults int    `json:"max_results"`
}

// MemoryConfig configures the memory system.
type MemoryConfig struct {
	DailyNotesDays         int     `json:"daily_notes_days"`
	AutoSummarizeThreshold float64 `json:"auto_summarize_threshold"`
}

// HeartbeatConfig configures the heartbeat service.
type HeartbeatConfig struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"interval_seconds"`
}

// CompactConfig configures strategic compaction.
type CompactConfig struct {
	Enabled          bool     `json:"enabled"`
	BoundaryPatterns []string `json:"boundary_patterns"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Workspace: filepath.Join(home, ".domiclaw", "workspace"),
		Agents: AgentsConfig{
			Model:             "claude-sonnet-4-20250514",
			MaxTokens:         8192,
			Temperature:       0.7,
			MaxToolIterations: 20,
		},
		Providers: ProvidersConfig{
			// API keys should come from environment variables
			Anthropic: &ProviderConfig{},
		},
		Tools: ToolsConfig{
			Web: WebToolsConfig{
				Search: SearchConfig{
					MaxResults: 5,
				},
			},
		},
		Memory: MemoryConfig{
			DailyNotesDays:         3,
			AutoSummarizeThreshold: 0.75,
		},
		Heartbeat: HeartbeatConfig{
			Enabled:         false, // Disabled by default
			IntervalSeconds: 300,
		},
		StrategicCompact: CompactConfig{
			Enabled: true,
			BoundaryPatterns: []string{
				"Phase complete",
				"Moving to",
				"Task done",
				"Checkpoint",
			},
		},
	}
}

// ConfigPath returns the default config file path.
func ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".domiclaw", "config.json")
}

// Load loads configuration from the default path.
func Load() (*Config, error) {
	return LoadFrom(ConfigPath())
}

// LoadFrom loads configuration from a specific path.
func LoadFrom(path string) (*Config, error) {
	path = utils.ExpandPath(path)

	// Start with defaults
	cfg := DefaultConfig()

	// Try to load from file
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	// Parse JSON, overwriting defaults
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Expand paths
	cfg.Workspace = utils.ExpandPath(cfg.Workspace)

	return cfg, nil
}

// Save saves the configuration to the default path.
func (c *Config) Save() error {
	return c.SaveTo(ConfigPath())
}

// SaveTo saves the configuration to a specific path.
func (c *Config) SaveTo(path string) error {
	path = utils.ExpandPath(path)

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := utils.EnsureDir(dir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// WorkspacePath returns the expanded workspace path.
func (c *Config) WorkspacePath() string {
	return utils.ExpandPath(c.Workspace)
}

// MemoryDir returns the memory directory path.
func (c *Config) MemoryDir() string {
	return filepath.Join(c.WorkspacePath(), "memory")
}

// SessionsDir returns the sessions directory path.
func (c *Config) SessionsDir() string {
	return filepath.Join(c.WorkspacePath(), "sessions")
}

// GetAnthropicAPIKey returns the Anthropic API key.
// Priority: 1. Environment variable, 2. Config file
func (c *Config) GetAnthropicAPIKey() string {
	// 1. Try environment variable first (most secure)
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return key
	}

	// 2. Fall back to config file
	if c.Providers.Anthropic != nil && c.Providers.Anthropic.APIKey != "" {
		return c.Providers.Anthropic.APIKey
	}

	return ""
}

// GetOpenRouterAPIKey returns the OpenRouter API key.
func (c *Config) GetOpenRouterAPIKey() string {
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		return key
	}

	if c.Providers.OpenRouter != nil && c.Providers.OpenRouter.APIKey != "" {
		return c.Providers.OpenRouter.APIKey
	}

	return ""
}

// GetSearchAPIKey returns the web search API key.
func (c *Config) GetSearchAPIKey() string {
	if key := os.Getenv("BRAVE_API_KEY"); key != "" {
		return key
	}
	if key := os.Getenv("TAVILY_API_KEY"); key != "" {
		return key
	}

	return c.Tools.Web.Search.APIKey
}
