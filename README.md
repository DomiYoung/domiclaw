# DomiClaw

> Ultra-lightweight AI coding assistant in Go, inspired by [PicoClaw](https://github.com/sipeed/picoclaw)

## Features

- **Standalone Agent**: Direct LLM API calls, no external dependencies
- **Multi-Provider**: Anthropic and OpenRouter support
- **Memory System**: Long-term memory (MEMORY.md) + daily logs
- **Session Recovery**: Automatic gap analysis after context overflow
- **Built-in Tools**: File ops, code editing, command execution, web search
- **Secure**: API keys via environment variables (never stored in config)
- **Single Binary**: Cross-platform, ~10MB compiled

## Quick Start

```bash
# Install
git clone https://github.com/DomiYoung/domiclaw.git
cd domiclaw
make install

# Set API key
export ANTHROPIC_API_KEY="your-api-key"

# Initialize
domiclaw init

# Run
domiclaw run -m "Help me refactor main.go"
```

## Architecture

```
domiclaw/
├── cmd/domiclaw/       # CLI entry point
└── pkg/
    ├── agent/          # Agent loop core
    ├── config/         # Configuration management
    ├── memory/         # Memory system (MEMORY.md, daily logs)
    ├── session/        # Session management
    ├── providers/      # LLM providers (Anthropic, OpenRouter)
    ├── tools/          # Built-in tools
    ├── heartbeat/      # Heartbeat service
    ├── logger/         # Structured logging
    └── utils/          # Utility functions
```

## Memory Directory Structure

```
~/.domiclaw/
├── config.json            # Configuration
└── workspace/
    ├── MEMORY.md          # Long-term memory
    ├── resume-prompt.md   # Gap Analysis result
    ├── resume-trigger.json
    ├── memory/
    │   └── YYYYMM/
    │       └── YYYYMMDD.md  # Daily logs
    └── sessions/
        └── {id}.json      # Session history
```

## Commands

| Command | Description |
|---------|-------------|
| `domiclaw init` | Initialize workspace and config |
| `domiclaw run -m "prompt"` | Run agent with a prompt |
| `domiclaw run -w /path` | Run in specific workspace |
| `domiclaw resume` | Resume from context overflow |
| `domiclaw status` | Show current status |
| `domiclaw version` | Show version info |

## Configuration

Config file: `~/.domiclaw/config.json`

```json
{
  "workspace": "~/.domiclaw/workspace",
  "agents": {
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 8192,
    "temperature": 0.7,
    "max_tool_iterations": 20
  },
  "memory": {
    "daily_notes_days": 3,
    "auto_summarize_threshold": 0.75
  },
  "heartbeat": {
    "enabled": false,
    "interval_seconds": 300
  },
  "strategic_compact": {
    "enabled": true,
    "boundary_patterns": ["Phase complete", "Moving to", "Task done"]
  }
}
```

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ANTHROPIC_API_KEY` | Yes* | Anthropic API key |
| `OPENROUTER_API_KEY` | Yes* | OpenRouter API key (alternative to Anthropic) |
| `BRAVE_API_KEY` | No | Brave Search API key |
| `TAVILY_API_KEY` | No | Tavily Search API key (alternative to Brave) |

*One of `ANTHROPIC_API_KEY` or `OPENROUTER_API_KEY` is required.

**Security**: API keys are read from environment variables first. Never commit keys to config files.

## Built-in Tools

| Tool | Description |
|------|-------------|
| `read_file` | Read file contents |
| `write_file` | Write content to file |
| `edit_file` | Precise string replacement in files |
| `list_dir` | List directory contents |
| `exec` | Execute shell commands (with dangerous command blocking) |
| `web_search` | Search the web (Brave or Tavily) |

## Comparison

| Feature | DomiClaw | PicoClaw | OpenClaw |
|---------|----------|----------|----------|
| Language | Go | Go | TypeScript |
| RAM | ~10MB | <10MB | >1GB |
| Startup | <1s | <1s | >500s |
| Dependencies | None | None | Node.js |
| Memory System | ✅ | ✅ | ✅ |
| Context Recovery | ✅ | ❌ | ❌ |

## Development

```bash
# Build
make build

# Build for all platforms
make build-all

# Install locally
make install

# Run tests
make test

# Clean
make clean
```

## Inspiration

- [PicoClaw](https://github.com/sipeed/picoclaw) - Ultra-lightweight AI agent in Go
- [OpenClaw](https://github.com/openclaw/openclaw) - Memory architecture
- [Claude Code](https://claude.ai/code) - Agent patterns

## License

MIT License - see [LICENSE](LICENSE)
