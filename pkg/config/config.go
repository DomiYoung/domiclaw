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
	PiAgentPath      string          `json:"pi_agent_path"`
	Memory           MemoryConfig    `json:"memory"`
	Heartbeat        HeartbeatConfig `json:"heartbeat"`
	StrategicCompact CompactConfig   `json:"strategic_compact"`
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
		Workspace:   filepath.Join(home, ".domiclaw", "workspace"),
		PiAgentPath: "/opt/homebrew/bin/pi",
		Memory: MemoryConfig{
			DailyNotesDays:         3,
			AutoSummarizeThreshold: 0.75,
		},
		Heartbeat: HeartbeatConfig{
			Enabled:         true,
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
			// Return defaults if no config file
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
	cfg.PiAgentPath = utils.ExpandPath(cfg.PiAgentPath)

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
