# DomiClaw

> Ultra-lightweight Pi Agent runner in Go, inspired by [PicoClaw](https://github.com/sipeed/picoclaw)

## Features

- **Memory System**: Long-term memory (MEMORY.md) + daily logs
- **Session Management**: Conversation history with auto-summarization
- **Gap Analysis**: Context recovery after compaction
- **Heartbeat Service**: Periodic task checking
- **Strategic Compact**: Smart context compression at logical boundaries

## Architecture

```
domiclaw/
├── cmd/domiclaw/       # CLI entry point
└── pkg/
    ├── agent/          # Agent loop core
    ├── config/         # Configuration management
    ├── memory/         # Memory system (MEMORY.md, daily logs)
    ├── session/        # Session management with summarization
    ├── heartbeat/      # Heartbeat service
    ├── logger/         # Structured logging
    └── utils/          # Utility functions
```

## Memory Directory Structure

```
.domiclaw/
├── MEMORY.md              # Long-term memory
├── resume-prompt.md       # Gap Analysis result
├── resume-trigger.json    # Session recovery trigger
├── daily/
│   └── YYYYMM/
│       └── YYYYMMDD.md    # Daily logs
└── sessions/
    └── {session_key}.json # Session history
```

## Installation

```bash
# From source
git clone https://github.com/DomiYoung/domiclaw.git
cd domiclaw
make build

# Install to PATH
make install
```

## Usage

```bash
# Initialize workspace
domiclaw init

# Run with Pi Agent
domiclaw run --workspace /path/to/project

# Resume from last session
domiclaw resume

# Check status
domiclaw status
```

## Configuration

Config file: `~/.domiclaw/config.json`

```json
{
  "workspace": "~/.domiclaw/workspace",
  "pi_agent_path": "/opt/homebrew/bin/pi",
  "memory": {
    "daily_notes_days": 3,
    "auto_summarize_threshold": 0.75
  },
  "heartbeat": {
    "enabled": true,
    "interval_seconds": 300
  },
  "strategic_compact": {
    "enabled": true,
    "boundary_patterns": ["Phase complete", "Moving to", "Task done"]
  }
}
```

## Inspiration

- [PicoClaw](https://github.com/sipeed/picoclaw) - Ultra-lightweight AI agent in Go
- [OpenClaw](https://github.com/openclaw/openclaw) - Memory architecture (MEMORY.md, daily logs)
- [Pi Agent](https://github.com/mariozechner/pi-coding-agent) - The underlying coding agent

## License

MIT License - see [LICENSE](LICENSE)
