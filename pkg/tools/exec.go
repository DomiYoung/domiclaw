// Package tools provides command execution tools.
package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExecTool executes shell commands.
type ExecTool struct {
	Workspace string
	Timeout   time.Duration
}

// NewExecTool creates a new exec tool.
func NewExecTool(workspace string) *ExecTool {
	return &ExecTool{
		Workspace: workspace,
		Timeout:   120 * time.Second,
	}
}

func (t *ExecTool) Name() string { return "exec" }

func (t *ExecTool) Description() string {
	return "Execute a shell command and return its output. Use for running build commands, git operations, etc."
}

func (t *ExecTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"workdir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory for the command (optional)",
			},
		},
		"required": []string{"command"},
	}
}

// Dangerous command patterns to block
var dangerousPatterns = []string{
	"rm -rf /",
	"rm -rf /*",
	"rm -rf ~",
	"rm -rf $HOME",
	"mkfs.",
	"dd if=",
	":(){:|:&};:",
	"> /dev/sda",
	"chmod -R 777 /",
	"chown -R",
}

func (t *ExecTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("command must be a string")
	}

	// Security: check for dangerous commands
	cmdLower := strings.ToLower(command)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(cmdLower, strings.ToLower(pattern)) {
			return "", fmt.Errorf("dangerous command blocked: %s", pattern)
		}
	}

	workdir := t.Workspace
	if wd, ok := args["workdir"].(string); ok && wd != "" {
		workdir = wd
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()

	// Execute command
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workdir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Build result
	var result strings.Builder
	if stdout.Len() > 0 {
		result.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString("stderr:\n")
		result.WriteString(stderr.String())
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return result.String(), fmt.Errorf("command timed out after %v", t.Timeout)
		}
		return result.String(), fmt.Errorf("command failed: %w", err)
	}

	return result.String(), nil
}
