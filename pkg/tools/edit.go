// Package tools provides edit file tool.
package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EditFileTool performs precise string replacements in files.
type EditFileTool struct {
	Workspace string
}

func (t *EditFileTool) Name() string { return "edit_file" }

func (t *EditFileTool) Description() string {
	return `Perform exact string replacement in a file. Use this for precise edits.
The oldString must match exactly (including whitespace and indentation).
If replaceAll is true, all occurrences will be replaced.`
}

func (t *EditFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the file to edit",
			},
			"old_string": map[string]interface{}{
				"type":        "string",
				"description": "The exact string to find and replace",
			},
			"new_string": map[string]interface{}{
				"type":        "string",
				"description": "The string to replace it with",
			},
			"replace_all": map[string]interface{}{
				"type":        "boolean",
				"description": "If true, replace all occurrences (default: false)",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

func (t *EditFileTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok {
		return "", fmt.Errorf("path must be a string")
	}

	oldString, ok := args["old_string"].(string)
	if !ok {
		return "", fmt.Errorf("old_string must be a string")
	}

	newString, ok := args["new_string"].(string)
	if !ok {
		return "", fmt.Errorf("new_string must be a string")
	}

	replaceAll := false
	if ra, ok := args["replace_all"].(bool); ok {
		replaceAll = ra
	}

	// Security: ensure path is within workspace
	if t.Workspace != "" {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("failed to resolve path: %w", err)
		}
		absWorkspace, err := filepath.Abs(t.Workspace)
		if err != nil {
			return "", fmt.Errorf("failed to resolve workspace: %w", err)
		}
		if !strings.HasPrefix(absPath, absWorkspace) {
			return "", fmt.Errorf("path must be within workspace")
		}
	}

	// Read file
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	original := string(content)

	// Check if oldString exists
	if !strings.Contains(original, oldString) {
		return "", fmt.Errorf("old_string not found in file")
	}

	// Count occurrences
	count := strings.Count(original, oldString)

	// If multiple occurrences and not replaceAll, error
	if count > 1 && !replaceAll {
		return "", fmt.Errorf("old_string found %d times. Use replace_all=true to replace all, or provide more context to make it unique", count)
	}

	// Perform replacement
	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(original, oldString, newString)
	} else {
		newContent = strings.Replace(original, oldString, newString, 1)
	}

	// Write back
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	if replaceAll && count > 1 {
		return fmt.Sprintf("Successfully replaced %d occurrences in %s", count, path), nil
	}
	return fmt.Sprintf("Successfully edited %s", path), nil
}
