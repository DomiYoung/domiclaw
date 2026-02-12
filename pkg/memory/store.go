// Package memory provides persistent memory management for DomiClaw.
// Inspired by OpenClaw and PicoClaw memory architectures.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DomiYoung/domiclaw/pkg/utils"
)

// Store manages persistent memory for the agent.
// Structure:
//
//	.domiclaw/
//	├── MEMORY.md              # Long-term memory
//	├── resume-prompt.md       # Gap Analysis result
//	├── resume-trigger.json    # Session recovery trigger
//	└── memory/
//	    └── YYYYMM/
//	        └── YYYYMMDD.md    # Daily logs
type Store struct {
	workspace  string
	memoryDir  string
	memoryFile string
}

// NewStore creates a new memory store with the given workspace path.
func NewStore(workspace string) *Store {
	memoryDir := filepath.Join(workspace, "memory")
	memoryFile := filepath.Join(workspace, "MEMORY.md")

	// Ensure memory directory exists
	utils.EnsureDir(memoryDir)

	return &Store{
		workspace:  workspace,
		memoryDir:  memoryDir,
		memoryFile: memoryFile,
	}
}

// getTodayFile returns the path to today's daily note.
// Format: memory/YYYYMM/YYYYMMDD.md
func (s *Store) getTodayFile() string {
	now := time.Now()
	monthDir := now.Format("200601")          // YYYYMM
	dayFile := now.Format("20060102") + ".md" // YYYYMMDD.md
	return filepath.Join(s.memoryDir, monthDir, dayFile)
}

// ReadLongTerm reads the long-term memory (MEMORY.md).
func (s *Store) ReadLongTerm() string {
	return utils.ReadFileString(s.memoryFile)
}

// WriteLongTerm writes content to the long-term memory file.
func (s *Store) WriteLongTerm(content string) error {
	return utils.WriteFileString(s.memoryFile, content)
}

// AppendLongTerm appends content to the long-term memory file.
func (s *Store) AppendLongTerm(content string) error {
	existing := s.ReadLongTerm()
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	return s.WriteLongTerm(existing + content)
}

// ReadToday reads today's daily note.
func (s *Store) ReadToday() string {
	return utils.ReadFileString(s.getTodayFile())
}

// AppendToday appends content to today's daily note.
// Creates the file with a date header if it doesn't exist.
func (s *Store) AppendToday(content string) error {
	todayFile := s.getTodayFile()
	existing := utils.ReadFileString(todayFile)

	var newContent string
	if existing == "" {
		// Add header for new day
		header := fmt.Sprintf("# %s\n\n", time.Now().Format("2006-01-02 Monday"))
		newContent = header + content
	} else {
		if !strings.HasSuffix(existing, "\n") {
			existing += "\n"
		}
		newContent = existing + "\n" + content
	}

	return utils.WriteFileString(todayFile, newContent)
}

// GetRecentDailyNotes returns daily notes from the last N days.
func (s *Store) GetRecentDailyNotes(days int) []DailyNote {
	var notes []DailyNote

	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -i)
		monthDir := date.Format("200601")
		dayFile := date.Format("20060102") + ".md"
		filePath := filepath.Join(s.memoryDir, monthDir, dayFile)

		content := utils.ReadFileString(filePath)
		if content != "" {
			notes = append(notes, DailyNote{
				Date:    date,
				Content: content,
			})
		}
	}

	return notes
}

// DailyNote represents a daily log entry.
type DailyNote struct {
	Date    time.Time
	Content string
}

// GetMemoryContext returns formatted memory context for injection into prompts.
func (s *Store) GetMemoryContext(recentDays int) string {
	var parts []string

	// Long-term memory
	longTerm := s.ReadLongTerm()
	if longTerm != "" {
		parts = append(parts, "## Long-term Memory\n\n"+longTerm)
	}

	// Recent daily notes
	notes := s.GetRecentDailyNotes(recentDays)
	if len(notes) > 0 {
		var notesContent string
		for i, note := range notes {
			if i > 0 {
				notesContent += "\n\n---\n\n"
			}
			notesContent += note.Content
		}
		parts = append(parts, "## Recent Daily Notes\n\n"+notesContent)
	}

	if len(parts) == 0 {
		return ""
	}

	return "# Memory\n\n" + strings.Join(parts, "\n\n---\n\n")
}

// Resume prompt management

// ResumePromptPath returns the path to the resume prompt file.
func (s *Store) ResumePromptPath() string {
	return filepath.Join(s.workspace, "resume-prompt.md")
}

// ResumeTriggerPath returns the path to the resume trigger file.
func (s *Store) ResumeTriggerPath() string {
	return filepath.Join(s.workspace, "resume-trigger.json")
}

// WriteResumePrompt writes the Gap Analysis result for session recovery.
func (s *Store) WriteResumePrompt(content string) error {
	return utils.WriteFileString(s.ResumePromptPath(), content)
}

// ReadResumePrompt reads the resume prompt if it exists.
func (s *Store) ReadResumePrompt() string {
	return utils.ReadFileString(s.ResumePromptPath())
}

// ClearResumePrompt removes the resume prompt file after successful recovery.
func (s *Store) ClearResumePrompt() error {
	path := s.ResumePromptPath()
	if utils.FileExists(path) {
		return os.Remove(path)
	}
	return nil
}

// WriteResumeTrigger writes the trigger file for session recovery.
func (s *Store) WriteResumeTrigger(sessionID, reason string) error {
	content := fmt.Sprintf(`{
  "timestamp": "%s",
  "session_id": "%s",
  "reason": "%s",
  "workspace": "%s"
}
`, time.Now().Format(time.RFC3339), sessionID, reason, s.workspace)
	return utils.WriteFileString(s.ResumeTriggerPath(), content)
}

// ClearResumeTrigger removes the resume trigger file.
func (s *Store) ClearResumeTrigger() error {
	path := s.ResumeTriggerPath()
	if utils.FileExists(path) {
		return os.Remove(path)
	}
	return nil
}

// HasPendingResume checks if there's a pending session to resume.
func (s *Store) HasPendingResume() bool {
	return utils.FileExists(s.ResumeTriggerPath())
}
