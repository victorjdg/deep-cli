package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type DeepSeekClient struct {
	apiKey string
	model  string
	apiURL string
	client *http.Client
}

func NewDeepSeekClient(apiKey, model, apiURL string) *DeepSeekClient {
	return &DeepSeekClient{
		apiKey: apiKey,
		model:  model,
		apiURL: apiURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (d *DeepSeekClient) Complete(ctx context.Context, messages []Message) (string, TokenUsage, error) {
	body := deepseekRequest{
		Model:       d.model,
		Messages:    messages,
		Temperature: 0.1,
		MaxTokens:   4096,
		Stream:      false,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", TokenUsage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", d.apiURL, bytes.NewReader(data))
	if err != nil {
		return "", TokenUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)

	// Use a client without its own timeout so the caller's context deadline is respected.
	noTimeoutClient := &http.Client{}
	resp, err := noTimeoutClient.Do(req)
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("failed to connect to DeepSeek API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", TokenUsage{}, fmt.Errorf("DeepSeek API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result deepseekResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", TokenUsage{}, err
	}

	if len(result.Choices) == 0 {
		return "", TokenUsage{}, fmt.Errorf("no response from DeepSeek API")
	}

	return result.Choices[0].Message.Content, result.Usage, nil
}

func (d *DeepSeekClient) CompleteWithTools(ctx context.Context, messages []Message, tools []ToolDefinition) (string, []ToolCall, TokenUsage, error) {
	body := deepseekRequest{
		Model:       d.model,
		Messages:    messages,
		Temperature: 0.1,
		MaxTokens:   4096,
		Stream:      false,
		Tools:       tools,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", nil, TokenUsage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", d.apiURL, bytes.NewReader(data))
	if err != nil {
		return "", nil, TokenUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)

	// Use a longer timeout for tool-calling requests.
	toolClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := toolClient.Do(req)
	if err != nil {
		return "", nil, TokenUsage{}, fmt.Errorf("failed to connect to DeepSeek API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", nil, TokenUsage{}, fmt.Errorf("DeepSeek API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result deepseekResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, TokenUsage{}, err
	}

	if len(result.Choices) == 0 {
		return "", nil, TokenUsage{}, fmt.Errorf("no response from DeepSeek API")
	}

	choice := result.Choices[0]
	return choice.Message.Content, choice.Message.ToolCalls, result.Usage, nil
}

func (d *DeepSeekClient) Stream(ctx context.Context, messages []Message) (<-chan StreamChunk, error) {
	body := deepseekRequest{
		Model:       d.model,
		Messages:    messages,
		Temperature: 0.1,
		MaxTokens:   4096,
		Stream:      true,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", d.apiURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)

	// Use a client without timeout for streaming.
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to DeepSeek API: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("DeepSeek API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		var usage TokenUsage

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()

			// SSE format: "data: {...}" or "data: [DONE]"
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				ch <- StreamChunk{Done: true, Usage: usage}
				return
			}

			var result deepseekResponse
			if err := json.Unmarshal([]byte(payload), &result); err != nil {
				ch <- StreamChunk{Err: err}
				return
			}

			if result.Usage.TotalTokens > 0 {
				usage = result.Usage
			}

			if len(result.Choices) > 0 && result.Choices[0].Delta.Content != "" {
				ch <- StreamChunk{Content: result.Choices[0].Delta.Content}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Err: err}
		}
	}()

	return ch, nil
}

func (d *DeepSeekClient) CheckConnection(ctx context.Context) error {
	if d.apiKey == "" {
		return fmt.Errorf("DeepSeek API key is not set")
	}
	// Validate connection by listing models.
	_, err := d.ListModels(ctx)
	return err
}

func (d *DeepSeekClient) ListModels(ctx context.Context) ([]string, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// The API base is the chat completions URL minus the path.
	baseURL := strings.TrimSuffix(d.apiURL, "/chat/completions")
	req, err := http.NewRequestWithContext(checkCtx, "GET", baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list DeepSeek models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("DeepSeek API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result deepseekModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]string, len(result.Data))
	for i, m := range result.Data {
		models[i] = m.ID
	}
	return models, nil
}
