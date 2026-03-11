package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OllamaClient struct {
	host   string
	model  string
	client *http.Client
}

func NewOllamaClient(host, model string) *OllamaClient {
	return &OllamaClient{
		host:  host,
		model: model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (o *OllamaClient) Complete(ctx context.Context, messages []Message) (string, TokenUsage, error) {
	body := ollamaRequest{
		Model:    o.model,
		Messages: messages,
		Stream:   false,
		Options: ollamaOptions{
			Temperature: 0.1,
			NumPredict:  4096,
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", TokenUsage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.host+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return "", TokenUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("failed to connect to Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", TokenUsage{}, fmt.Errorf("Ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", TokenUsage{}, err
	}

	usage := TokenUsage{
		PromptTokens:     result.PromptEvalCount,
		CompletionTokens: result.EvalCount,
		TotalTokens:      result.PromptEvalCount + result.EvalCount,
	}
	return result.Message.Content, usage, nil
}

func (o *OllamaClient) CompleteWithTools(ctx context.Context, messages []Message, tools []ToolDefinition) (string, []ToolCall, TokenUsage, error) {
	// Ollama does not support tool calling; fall back to regular Complete.
	content, usage, err := o.Complete(ctx, messages)
	return content, nil, usage, err
}

func (o *OllamaClient) Stream(ctx context.Context, messages []Message) (<-chan StreamChunk, error) {
	body := ollamaRequest{
		Model:    o.model,
		Messages: messages,
		Stream:   true,
		Options: ollamaOptions{
			Temperature: 0.1,
			NumPredict:  4096,
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.host+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Use a client without timeout for streaming.
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ollama: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("Ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var chunk ollamaResponse
			if err := json.Unmarshal(line, &chunk); err != nil {
				ch <- StreamChunk{Err: err}
				return
			}

			if chunk.Done {
				ch <- StreamChunk{
					Done: true,
					Usage: TokenUsage{
						PromptTokens:     chunk.PromptEvalCount,
						CompletionTokens: chunk.EvalCount,
						TotalTokens:      chunk.PromptEvalCount + chunk.EvalCount,
					},
				}
				return
			}

			ch <- StreamChunk{Content: chunk.Message.Content}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Err: err}
		}
	}()

	return ch, nil
}

func (o *OllamaClient) CheckConnection(ctx context.Context) error {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, "GET", o.host+"/api/tags", nil)
	if err != nil {
		return err
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot connect to Ollama at %s: %w", o.host, err)
	}
	resp.Body.Close()
	return nil
}

func (o *OllamaClient) ListModels(ctx context.Context) ([]string, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, "GET", o.host+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to Ollama: %w", err)
	}
	defer resp.Body.Close()

	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}

	models := make([]string, len(tags.Models))
	for i, m := range tags.Models {
		models[i] = m.Name
	}
	return models, nil
}
