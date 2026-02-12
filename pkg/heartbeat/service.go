// Package heartbeat provides periodic task checking for DomiClaw.
package heartbeat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DomiYoung/domiclaw/pkg/logger"
	"github.com/DomiYoung/domiclaw/pkg/utils"
)

// Callback is called on each heartbeat with a prompt.
// Returns the response and any error.
type Callback func(prompt string) (string, error)

// Service manages periodic heartbeat checks.
type Service struct {
	workspace   string
	onHeartbeat Callback
	interval    time.Duration
	enabled     bool
	mu          sync.RWMutex
	stopChan    chan struct{}
	running     bool
}

// NewService creates a new heartbeat service.
func NewService(workspace string, callback Callback, intervalSec int, enabled bool) *Service {
	return &Service{
		workspace:   workspace,
		onHeartbeat: callback,
		interval:    time.Duration(intervalSec) * time.Second,
		enabled:     enabled,
		stopChan:    make(chan struct{}),
	}
}

// Start starts the heartbeat service.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	if !s.enabled {
		s.mu.Unlock()
		return fmt.Errorf("heartbeat service is disabled")
	}
	s.running = true
	s.stopChan = make(chan struct{})
	s.mu.Unlock()

	logger.Info("Heartbeat service started")
	go s.runLoop(ctx)

	return nil
}

// Stop stops the heartbeat service.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	close(s.stopChan)
	s.running = false
	logger.Info("Heartbeat service stopped")
}

// runLoop runs the heartbeat check loop.
func (s *Service) runLoop(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.checkHeartbeat()
		}
	}
}

// checkHeartbeat performs a single heartbeat check.
func (s *Service) checkHeartbeat() {
	s.mu.RLock()
	if !s.enabled || !s.running {
		s.mu.RUnlock()
		return
	}
	callback := s.onHeartbeat
	s.mu.RUnlock()

	if callback == nil {
		return
	}

	prompt := s.buildPrompt()

	logger.DebugCF("heartbeat", "Executing heartbeat check", nil)

	_, err := callback(prompt)
	if err != nil {
		logger.ErrorCF("heartbeat", "Heartbeat check failed", map[string]interface{}{
			"error": err.Error(),
		})
		s.log(fmt.Sprintf("Heartbeat error: %v", err))
	}
}

// buildPrompt builds the heartbeat prompt.
func (s *Service) buildPrompt() string {
	// Read heartbeat notes if they exist
	notesFile := filepath.Join(s.workspace, "memory", "HEARTBEAT.md")
	notes := utils.ReadFileString(notesFile)

	now := time.Now().Format("2006-01-02 15:04")

	prompt := fmt.Sprintf(`# Heartbeat Check

Current time: %s

Check if there are any tasks I should be aware of or actions I should take.
Review the memory file for any important updates or changes.
Be proactive in identifying potential issues or improvements.

`, now)

	if notes != "" {
		prompt += "## Heartbeat Notes\n\n" + notes
	}

	return prompt
}

// log writes a message to the heartbeat log.
func (s *Service) log(message string) {
	logFile := filepath.Join(s.workspace, "memory", "heartbeat.log")

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	f.WriteString(fmt.Sprintf("[%s] %s\n", timestamp, message))
}
