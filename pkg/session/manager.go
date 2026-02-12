// Package session provides conversation session management for DomiClaw.
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DomiYoung/domiclaw/pkg/utils"
)

// Message represents a conversation message.
type Message struct {
	Role       string    `json:"role"` // user, assistant, system, tool
	Content    string    `json:"content"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// Session represents a conversation session.
type Session struct {
	ID       string    `json:"id"`
	Messages []Message `json:"messages"`
	Summary  string    `json:"summary,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

// Manager manages conversation sessions.
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	storage  string
}

// NewManager creates a new session manager.
func NewManager(storageDir string) *Manager {
	utils.EnsureDir(storageDir)

	mgr := &Manager{
		sessions: make(map[string]*Session),
		storage:  storageDir,
	}

	// Load existing sessions
	mgr.loadSessions()

	return mgr
}

// GetOrCreate gets an existing session or creates a new one.
func (m *Manager) GetOrCreate(id string) *Session {
	m.mu.RLock()
	session, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		m.mu.Lock()
		session = &Session{
			ID:       id,
			Messages: []Message{},
			Created:  time.Now(),
			Updated:  time.Now(),
		}
		m.sessions[id] = session
		m.mu.Unlock()
	}

	return session
}

// AddMessage adds a message to the session.
func (m *Manager) AddMessage(sessionID, role, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		session = &Session{
			ID:       sessionID,
			Messages: []Message{},
			Created:  time.Now(),
		}
		m.sessions[sessionID] = session
	}

	session.Messages = append(session.Messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
	session.Updated = time.Now()
}

// GetHistory returns the message history for a session.
func (m *Manager) GetHistory(sessionID string) []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return []Message{}
	}

	// Return a copy
	history := make([]Message, len(session.Messages))
	copy(history, session.Messages)
	return history
}

// GetSummary returns the session summary.
func (m *Manager) GetSummary(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return ""
	}
	return session.Summary
}

// SetSummary sets the session summary.
func (m *Manager) SetSummary(sessionID, summary string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if ok {
		session.Summary = summary
		session.Updated = time.Now()
	}
}

// TruncateHistory keeps only the last N messages.
func (m *Manager) TruncateHistory(sessionID string, keepLast int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return
	}

	if len(session.Messages) <= keepLast {
		return
	}

	session.Messages = session.Messages[len(session.Messages)-keepLast:]
	session.Updated = time.Now()
}

// Save persists a session to disk.
func (m *Manager) Save(session *Session) error {
	if m.storage == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	path := filepath.Join(m.storage, session.ID+".json")
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// SaveAll persists all sessions to disk.
func (m *Manager) SaveAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, session := range m.sessions {
		path := filepath.Join(m.storage, session.ID+".json")
		data, err := json.MarshalIndent(session, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return err
		}
	}

	return nil
}

// loadSessions loads all sessions from the storage directory.
func (m *Manager) loadSessions() {
	if m.storage == "" {
		return
	}

	entries, err := os.ReadDir(m.storage)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(m.storage, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}

		m.sessions[session.ID] = &session
	}
}

// EstimateTokens estimates the token count for messages.
// Uses a simple heuristic: ~4 characters per token.
func EstimateTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content) / 4
	}
	return total
}
