package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Brave struct {
	apiKey string
}

func NewBrave() *Brave {
	return &Brave{apiKey: os.Getenv("BRAVE_SEARCH_API_KEY")}
}

func (b *Brave) Name() string { return "brave" }

func (b *Brave) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	if b.apiKey == "" {
		return nil, fmt.Errorf("BRAVE_SEARCH_API_KEY environment variable is not set")
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("count", strconv.Itoa(maxResults))

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", "https://api.search.brave.com/res/v1/web/search?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave returned status %d", resp.StatusCode)
	}

	var result braveResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var results []Result
	for _, r := range result.Web.Results {
		results = append(results, Result{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Description,
		})
	}
	return results, nil
}

type braveResponse struct {
	Web braveWebResults `json:"web"`
}

type braveWebResults struct {
	Results []braveResult `json:"results"`
}

type braveResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}
