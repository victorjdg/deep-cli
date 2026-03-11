package search

import (
	"context"
	"fmt"
	"strings"
)

// Result is a single search hit.
type Result struct {
	Title   string
	URL     string
	Snippet string
}

// Engine can execute a web search query.
type Engine interface {
	Name() string
	Search(ctx context.Context, query string, maxResults int) ([]Result, error)
}

// FormatResults produces a plain-text block suitable for LLM context.
func FormatResults(query string, results []Result) string {
	if len(results) == 0 {
		return fmt.Sprintf("No web results found for: %s", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Web search results for \"%s\":\n\n", query))
	for i, r := range results {
		b.WriteString(fmt.Sprintf("%d. %s\n   URL: %s\n   %s\n\n", i+1, r.Title, r.URL, r.Snippet))
	}
	return b.String()
}
