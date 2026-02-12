// Package tools provides web search tool.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WebSearchTool searches the web using Brave or Tavily API.
type WebSearchTool struct {
	APIKey     string
	Provider   string // "brave" or "tavily"
	MaxResults int
	client     *http.Client
}

// NewWebSearchTool creates a new web search tool.
func NewWebSearchTool(apiKey string, maxResults int) *WebSearchTool {
	provider := "brave"
	if strings.HasPrefix(apiKey, "tvly-") {
		provider = "tavily"
	}

	return &WebSearchTool{
		APIKey:     apiKey,
		Provider:   provider,
		MaxResults: maxResults,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return "Search the web for current information. Returns relevant snippets and URLs."
}

func (t *WebSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query",
			},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok {
		return "", fmt.Errorf("query must be a string")
	}

	if t.APIKey == "" {
		return "Web search not configured. Set BRAVE_API_KEY or TAVILY_API_KEY environment variable.", nil
	}

	switch t.Provider {
	case "tavily":
		return t.searchTavily(ctx, query)
	default:
		return t.searchBrave(ctx, query)
	}
}

// Brave Search API
func (t *WebSearchTool) searchBrave(ctx context.Context, query string) (string, error) {
	apiURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), t.MaxResults)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.APIKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("search failed: %s - %s", resp.Status, string(body))
	}

	var result braveSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return t.formatBraveResults(result), nil
}

type braveSearchResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

func (t *WebSearchTool) formatBraveResults(result braveSearchResponse) string {
	var sb strings.Builder
	sb.WriteString("Search Results:\n\n")

	for i, r := range result.Web.Results {
		if i >= t.MaxResults {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("   URL: %s\n", r.URL))
		sb.WriteString(fmt.Sprintf("   %s\n\n", r.Description))
	}

	if len(result.Web.Results) == 0 {
		sb.WriteString("No results found.")
	}

	return sb.String()
}

// Tavily Search API
func (t *WebSearchTool) searchTavily(ctx context.Context, query string) (string, error) {
	apiURL := "https://api.tavily.com/search"

	reqBody := map[string]interface{}{
		"api_key":     t.APIKey,
		"query":       query,
		"max_results": t.MaxResults,
	}

	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(reqData)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("search failed: %s - %s", resp.Status, string(body))
	}

	var result tavilySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return t.formatTavilyResults(result), nil
}

type tavilySearchResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

func (t *WebSearchTool) formatTavilyResults(result tavilySearchResponse) string {
	var sb strings.Builder
	sb.WriteString("Search Results:\n\n")

	for i, r := range result.Results {
		if i >= t.MaxResults {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("   URL: %s\n", r.URL))
		// Truncate content if too long
		content := r.Content
		if len(content) > 300 {
			content = content[:297] + "..."
		}
		sb.WriteString(fmt.Sprintf("   %s\n\n", content))
	}

	if len(result.Results) == 0 {
		sb.WriteString("No results found.")
	}

	return sb.String()
}
