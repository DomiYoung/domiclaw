// Package tools provides grep content search tool.
package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GrepTool searches file contents using regular expressions.
type GrepTool struct {
	Workspace string
}

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Description() string {
	return `Search file contents using a regular expression pattern.
Returns matching lines with file paths and line numbers.
Supports standard regex syntax (e.g., "log.*Error", "func\s+\w+").`
}

func (t *GrepTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Regular expression pattern to search for",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory to search in (defaults to workspace)",
			},
			"include": map[string]interface{}{
				"type":        "string",
				"description": "File pattern to include (e.g., '*.go', '*.{ts,tsx}')",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *GrepTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("pattern must be a string")
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	basePath := t.Workspace
	if p, ok := args["path"].(string); ok && p != "" {
		basePath = p
	}

	includePattern := ""
	if inc, ok := args["include"].(string); ok {
		includePattern = inc
	}

	var results []grepMatch
	maxResults := 100

	err = filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip directories
		if info.IsDir() {
			// Skip hidden directories and common non-code directories
			if strings.HasPrefix(info.Name(), ".") ||
				info.Name() == "node_modules" ||
				info.Name() == "vendor" ||
				info.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip binary and large files
		if info.Size() > 1024*1024 { // Skip files > 1MB
			return nil
		}

		// Check include pattern
		if includePattern != "" && !matchIncludePattern(info.Name(), includePattern) {
			return nil
		}

		// Search file
		matches, err := searchFile(path, re)
		if err != nil {
			return nil // Skip files we can't read
		}

		results = append(results, matches...)

		if len(results) >= maxResults {
			return filepath.SkipAll
		}

		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return "", fmt.Errorf("search failed: %w", err)
	}

	// Format results
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d matches:\n\n", len(results)))

	for _, m := range results {
		sb.WriteString(fmt.Sprintf("%s:%d: %s\n", m.File, m.Line, m.Content))
	}

	if len(results) >= maxResults {
		sb.WriteString("\n... (results truncated)")
	}

	return sb.String(), nil
}

type grepMatch struct {
	File    string
	Line    int
	Content string
}

func searchFile(path string, re *regexp.Regexp) ([]grepMatch, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var matches []grepMatch
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if re.MatchString(line) {
			// Truncate long lines
			content := line
			if len(content) > 200 {
				content = content[:197] + "..."
			}

			matches = append(matches, grepMatch{
				File:    path,
				Line:    lineNum,
				Content: strings.TrimSpace(content),
			})
		}
	}

	return matches, scanner.Err()
}

// matchIncludePattern checks if a filename matches an include pattern.
// Supports patterns like "*.go", "*.{ts,tsx}"
func matchIncludePattern(filename, pattern string) bool {
	// Handle {a,b,c} syntax
	if strings.Contains(pattern, "{") {
		start := strings.Index(pattern, "{")
		end := strings.Index(pattern, "}")
		if start < end {
			prefix := pattern[:start]
			suffix := pattern[end+1:]
			options := strings.Split(pattern[start+1:end], ",")

			for _, opt := range options {
				subPattern := prefix + opt + suffix
				if matched, _ := filepath.Match(subPattern, filename); matched {
					return true
				}
			}
			return false
		}
	}

	matched, _ := filepath.Match(pattern, filename)
	return matched
}
