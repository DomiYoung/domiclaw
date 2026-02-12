// Package tools provides glob search tool.
package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// fileInfo holds file path and modification time for sorting.
type fileInfo struct {
	Path    string
	ModTime time.Time
}

// GlobTool searches for files matching a glob pattern.
type GlobTool struct {
	Workspace string
}

func (t *GlobTool) Name() string { return "glob" }

func (t *GlobTool) Description() string {
	return `Search for files matching a glob pattern. Supports patterns like:
- "**/*.go" - All Go files
- "src/**/*.ts" - TypeScript files in src
- "*.md" - Markdown files in current directory
Returns matching file paths sorted by modification time (newest first).`
}

func (t *GlobTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Glob pattern to match files (e.g., '**/*.go', 'src/**/*.ts')",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Base directory to search in (defaults to workspace)",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *GlobTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("pattern must be a string")
	}

	basePath := t.Workspace
	if p, ok := args["path"].(string); ok && p != "" {
		basePath = p
	}

	// Handle ** patterns by walking the directory tree
	var matches []fileInfo

	if strings.Contains(pattern, "**") {
		// Walk directory tree for ** patterns
		err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors
			}

			if info.IsDir() {
				return nil
			}

			// Convert ** pattern to check
			relPath, _ := filepath.Rel(basePath, path)
			if matchGlobPattern(pattern, relPath) {
				matches = append(matches, fileInfo{
					Path:    path,
					ModTime: info.ModTime(),
				})
			}

			return nil
		})

		if err != nil {
			return "", fmt.Errorf("failed to search: %w", err)
		}
	} else {
		// Simple glob pattern
		fullPattern := filepath.Join(basePath, pattern)
		paths, err := filepath.Glob(fullPattern)
		if err != nil {
			return "", fmt.Errorf("invalid glob pattern: %w", err)
		}

		for _, path := range paths {
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if !info.IsDir() {
				matches = append(matches, fileInfo{
					Path:    path,
					ModTime: info.ModTime(),
				})
			}
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].ModTime.After(matches[j].ModTime)
	})

	// Format results
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d files:\n\n", len(matches)))

	for i, f := range matches {
		if i >= 100 { // Limit output
			sb.WriteString(fmt.Sprintf("\n... and %d more files", len(matches)-100))
			break
		}
		sb.WriteString(f.Path + "\n")
	}

	return sb.String(), nil
}

// matchGlobPattern matches a path against a ** glob pattern
func matchGlobPattern(pattern, path string) bool {
	// Normalize separators
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)

	// Split pattern and path
	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")

	return matchParts(patternParts, pathParts)
}

func matchParts(pattern, path []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}

	if pattern[0] == "**" {
		// ** matches zero or more directories
		if len(pattern) == 1 {
			return true
		}

		// Try matching ** against 0, 1, 2, ... path segments
		for i := 0; i <= len(path); i++ {
			if matchParts(pattern[1:], path[i:]) {
				return true
			}
		}
		return false
	}

	if len(path) == 0 {
		return false
	}

	// Match current segment
	matched, _ := filepath.Match(pattern[0], path[0])
	if !matched {
		return false
	}

	return matchParts(pattern[1:], path[1:])
}
