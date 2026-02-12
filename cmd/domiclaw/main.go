// DomiClaw - Ultra-lightweight AI coding assistant
// Inspired by PicoClaw and OpenClaw
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/DomiYoung/domiclaw/pkg/agent"
	"github.com/DomiYoung/domiclaw/pkg/config"
	"github.com/DomiYoung/domiclaw/pkg/heartbeat"
	"github.com/DomiYoung/domiclaw/pkg/logger"
	"github.com/DomiYoung/domiclaw/pkg/memory"
	"github.com/DomiYoung/domiclaw/pkg/utils"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "version", "-v", "--version":
		fmt.Printf("domiclaw %s (commit: %s, built: %s)\n", Version, Commit, BuildTime)
	case "init":
		runInit()
	case "run":
		runAgent(os.Args[2:])
	case "resume":
		runResume()
	case "status":
		runStatus()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`DomiClaw - Ultra-lightweight AI coding assistant

Usage: domiclaw <command> [options]

Commands:
  init      Initialize workspace and config
  run       Run agent with a prompt
  resume    Resume from last session (after context overflow)
  status    Show current status
  version   Show version information
  help      Show this help message

Examples:
  domiclaw init
  domiclaw run -m "Help me refactor this code"
  domiclaw run --workspace /path/to/project
  domiclaw resume

Environment Variables:
  ANTHROPIC_API_KEY    Anthropic API key
  ANTHROPIC_BASE_URL   Custom Anthropic proxy (e.g. https://api.like-ai.cc)
  HONOURSOFT_API_KEY   Honoursoft proxy API key (OpenAI-compatible)
  HONOURSOFT_BASE_URL  Honoursoft proxy base URL
  OPENROUTER_API_KEY   OpenRouter API key
  TAVILY_API_KEY       Tavily search API key
  TAVILY_API_KEY_1~5   Tavily keys for rotation (auto-random)
  BRAVE_API_KEY        Brave Search API key

Configuration: ~/.domiclaw/config.json
`)
}

func runInit() {
	logger.Info("Initializing DomiClaw...")

	// Load or create default config
	cfg := config.DefaultConfig()

	// Ensure directories exist
	utils.EnsureDir(cfg.WorkspacePath())
	utils.EnsureDir(cfg.MemoryDir())
	utils.EnsureDir(cfg.SessionsDir())

	// Save config
	if err := cfg.Save(); err != nil {
		logger.ErrorF("Failed to save config", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	// Create initial MEMORY.md
	mem := memory.NewStore(cfg.WorkspacePath())
	if mem.ReadLongTerm() == "" {
		initialMemory := `# DomiClaw Memory

## Identity
- I am DomiClaw, an AI coding assistant with persistent memory
- I remember context across sessions through this file

## Preferences
- (Add your preferences here)

## Important Information
- (Add important info here)
`
		mem.WriteLongTerm(initialMemory)
	}

	logger.Info("Initialization complete!")
	fmt.Printf(`
DomiClaw initialized successfully!

Workspace: %s
Config:    %s

Next steps:
1. Set your API key: export ANTHROPIC_API_KEY="your-key"
2. Run: domiclaw run -m "Your prompt here"
`, cfg.WorkspacePath(), config.ConfigPath())
}

func runAgent(args []string) {
	// Parse arguments
	var prompt string
	var workspace string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-m", "--message":
			if i+1 < len(args) {
				prompt = args[i+1]
				i++
			}
		case "-w", "--workspace":
			if i+1 < len(args) {
				workspace = args[i+1]
				i++
			}
		}
	}

	if prompt == "" {
		fmt.Println("Error: No prompt provided. Use -m \"your prompt\"")
		os.Exit(1)
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		logger.ErrorF("Failed to load config", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	// Override workspace if provided
	if workspace != "" {
		cfg.Workspace = workspace
	}

	// Create agent loop
	loop, err := agent.NewLoop(cfg)
	if err != nil {
		logger.ErrorF("Failed to create agent", map[string]interface{}{
			"error": err.Error(),
		})
		fmt.Printf("\nError: %s\n", err.Error())
		fmt.Println("\nMake sure ANTHROPIC_API_KEY is set:")
		fmt.Println("  export ANTHROPIC_API_KEY=\"your-api-key\"")
		os.Exit(1)
	}

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start heartbeat if enabled
	var hb *heartbeat.Service
	if cfg.Heartbeat.Enabled {
		hb = heartbeat.NewService(
			cfg.WorkspacePath(),
			func(p string) (string, error) {
				logger.DebugCF("heartbeat", "Heartbeat triggered", nil)
				return "", nil
			},
			cfg.Heartbeat.IntervalSeconds,
			true,
		)
		hb.Start(ctx)
		defer hb.Stop()
	}

	// Run in goroutine to handle signals
	errChan := make(chan error, 1)
	go func() {
		errChan <- loop.Run(ctx, prompt)
	}()

	// Wait for completion or signal
	select {
	case err := <-errChan:
		if err != nil {
			logger.ErrorF("Agent error", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}
	case sig := <-sigChan:
		logger.InfoF("Received signal, shutting down", map[string]interface{}{
			"signal": sig.String(),
		})
		loop.Stop()
		cancel()
	}

	logger.Info("DomiClaw finished")
}

func runResume() {
	cfg, err := config.Load()
	if err != nil {
		logger.ErrorF("Failed to load config", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	mem := memory.NewStore(cfg.WorkspacePath())

	if !mem.HasPendingResume() {
		fmt.Println("No pending session to resume.")
		os.Exit(0)
	}

	resumePrompt := mem.ReadResumePrompt()
	if resumePrompt == "" {
		fmt.Println("Resume trigger found but no resume prompt. Creating default...")
		resumePrompt = "Resume previous session. Check MEMORY.md and daily notes for context."
	}

	fmt.Println("Resuming session...")

	// Clear the trigger before running
	mem.ClearResumeTrigger()

	// Run with the resume prompt
	runAgent([]string{"-m", resumePrompt})
}

func runStatus() {
	cfg, err := config.Load()
	if err != nil {
		logger.ErrorF("Failed to load config", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	mem := memory.NewStore(cfg.WorkspacePath())

	// Check API key and provider
	apiKeyStatus := "not set"
	providerName := "none"
	if cfg.GetAnthropicAPIKey() != "" {
		apiKeyStatus = "configured"
		if base := cfg.GetAnthropicAPIBase(); base != "" {
			providerName = fmt.Sprintf("anthropic (proxy: %s)", base)
		} else {
			providerName = "anthropic (direct)"
		}
	} else if cfg.GetHonoursoftAPIKey() != "" {
		apiKeyStatus = "configured"
		providerName = fmt.Sprintf("honoursoft (%s)", cfg.GetHonoursoftAPIBase())
	} else if cfg.GetOpenRouterAPIKey() != "" {
		apiKeyStatus = "configured"
		providerName = "openrouter"
	}

	// Check search key
	searchKeyStatus := "not set"
	if cfg.GetSearchAPIKey() != "" {
		searchKeyStatus = "configured"
	}

	fmt.Printf(`DomiClaw Status
===============

Workspace:      %s
Config:         %s
Model:          %s

Provider:       %s
API Key:        %s
Search Key:     %s

Memory:
  Long-term:    %v
  Daily dir:    %s

Heartbeat:      %s (every %ds)
Strategic:      %s

Pending Resume: %v
`,
		cfg.WorkspacePath(),
		config.ConfigPath(),
		cfg.Agents.Model,
		providerName,
		apiKeyStatus,
		searchKeyStatus,
		mem.ReadLongTerm() != "",
		cfg.MemoryDir(),
		boolToStatus(cfg.Heartbeat.Enabled),
		cfg.Heartbeat.IntervalSeconds,
		boolToStatus(cfg.StrategicCompact.Enabled),
		mem.HasPendingResume(),
	)
}

func boolToStatus(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
