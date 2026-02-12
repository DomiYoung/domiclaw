// Package agent provides the core agent loop for DomiClaw.
package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/DomiYoung/domiclaw/pkg/config"
	"github.com/DomiYoung/domiclaw/pkg/logger"
	"github.com/DomiYoung/domiclaw/pkg/memory"
	"github.com/DomiYoung/domiclaw/pkg/session"
)

// Loop manages the Pi Agent execution loop.
type Loop struct {
	cfg      *config.Config
	memory   *memory.Store
	sessions *session.Manager

	running     bool
	mu          sync.Mutex
	stopChan    chan struct{}
	currentProc *os.Process
}

// NewLoop creates a new agent loop.
func NewLoop(cfg *config.Config) *Loop {
	return &Loop{
		cfg:      cfg,
		memory:   memory.NewStore(cfg.WorkspacePath()),
		sessions: session.NewManager(cfg.SessionsDir()),
		stopChan: make(chan struct{}),
	}
}

// Run starts the agent loop.
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

	// Inject memory context
	memoryCtx := l.memory.GetMemoryContext(l.cfg.Memory.DailyNotesDays)
	if memoryCtx != "" {
		initialPrompt = memoryCtx + "\n\n---\n\n" + initialPrompt
	}

	return l.runPiAgent(ctx, initialPrompt)
}

// Stop stops the agent loop.
func (l *Loop) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.running {
		return
	}

	close(l.stopChan)

	// Terminate the Pi Agent process if running
	if l.currentProc != nil {
		l.currentProc.Signal(os.Interrupt)
	}
}

// runPiAgent executes the Pi Agent with the given prompt.
func (l *Loop) runPiAgent(ctx context.Context, prompt string) error {
	logger.InfoCF("agent", "Starting Pi Agent", map[string]interface{}{
		"pi_path":   l.cfg.PiAgentPath,
		"workspace": l.cfg.WorkspacePath(),
	})

	// Build command
	args := []string{
		"--print",
		"--verbose",
		"--workspace", l.cfg.WorkspacePath(),
		"--message", prompt,
	}

	cmd := exec.CommandContext(ctx, l.cfg.PiAgentPath, args...)
	cmd.Dir = l.cfg.WorkspacePath()

	// Set up pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Pi Agent: %w", err)
	}

	l.mu.Lock()
	l.currentProc = cmd.Process
	l.mu.Unlock()

	// Stream output
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		l.streamOutput(stdout, os.Stdout, "stdout")
	}()

	go func() {
		defer wg.Done()
		l.streamOutput(stderr, os.Stderr, "stderr")
	}()

	// Wait for completion
	wg.Wait()
	err = cmd.Wait()

	l.mu.Lock()
	l.currentProc = nil
	l.mu.Unlock()

	if err != nil {
		// Check if this was a context length error
		if l.detectContextOverflow(err) {
			return l.handleContextOverflow(ctx)
		}
		return fmt.Errorf("Pi Agent exited with error: %w", err)
	}

	return nil
}

// streamOutput streams output from a reader to a writer.
func (l *Loop) streamOutput(r io.Reader, w io.Writer, name string) {
	scanner := bufio.NewScanner(r)
	// Increase buffer size for long lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(w, line)

		// Check for strategic compact boundary
		if l.cfg.StrategicCompact.Enabled {
			l.checkStrategicBoundary(line)
		}
	}
}

// checkStrategicBoundary checks if a line indicates a strategic compact boundary.
func (l *Loop) checkStrategicBoundary(line string) {
	for _, pattern := range l.cfg.StrategicCompact.BoundaryPatterns {
		if strings.Contains(line, pattern) {
			logger.InfoCF("agent", "Strategic boundary detected", map[string]interface{}{
				"pattern": pattern,
			})
			// Log to daily notes
			l.memory.AppendToday(fmt.Sprintf("## Strategic Boundary: %s\n\nDetected at %s\n",
				pattern, time.Now().Format("15:04:05")))
			return
		}
	}
}

// detectContextOverflow checks if the error indicates context overflow.
func (l *Loop) detectContextOverflow(err error) bool {
	errStr := err.Error()
	overflowPatterns := []string{
		"context_length_exceeded",
		"maximum context length",
		"token limit",
		"too many tokens",
	}

	for _, pattern := range overflowPatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}

	return false
}

// handleContextOverflow handles context overflow by triggering recovery.
func (l *Loop) handleContextOverflow(ctx context.Context) error {
	logger.WarnCF("agent", "Context overflow detected, initiating recovery", nil)

	// Generate session ID
	sessionID := fmt.Sprintf("session_%d", time.Now().Unix())

	// Write resume trigger
	if err := l.memory.WriteResumeTrigger(sessionID, "context_overflow"); err != nil {
		logger.ErrorCF("agent", "Failed to write resume trigger", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Generate gap analysis prompt
	gapPrompt := l.generateGapAnalysisPrompt()
	if err := l.memory.WriteResumePrompt(gapPrompt); err != nil {
		logger.ErrorCF("agent", "Failed to write resume prompt", map[string]interface{}{
			"error": err.Error(),
		})
	}

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
