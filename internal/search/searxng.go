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

type SearXNG struct {
	host string
}

func NewSearXNG() *SearXNG {
	return &SearXNG{host: os.Getenv("SEARXNG_HOST")}
}

func (s *SearXNG) Name() string { return "searxng" }

func (s *SearXNG) Search(ctx context.Context, query string, maxResults int) ([]Result, error) {
	if s.host == "" {
		return nil, fmt.Errorf("SEARXNG_HOST environment variable is not set")
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("number_of_results", strconv.Itoa(maxResults))

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", s.host+"/search?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng returned status %d", resp.StatusCode)
	}

	var result searxngResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var results []Result
	for i, r := range result.Results {
		if i >= maxResults {
			break
		}
		results = append(results, Result{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}
	return results, nil
}

type searxngResponse struct {
	Results []searxngResult `json:"results"`
}

type searxngResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}
