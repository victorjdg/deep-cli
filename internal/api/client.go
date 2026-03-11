package api

import (
	"context"

	"github.com/victorjdg/deep-cli/internal/config"
)

// Client defines the interface for interacting with an LLM backend.
type Client interface {
	Complete(ctx context.Context, messages []Message) (string, TokenUsage, error)
	CompleteWithTools(ctx context.Context, messages []Message, tools []ToolDefinition) (string, []ToolCall, TokenUsage, error)
	Stream(ctx context.Context, messages []Message) (<-chan StreamChunk, error)
	CheckConnection(ctx context.Context) error
	ListModels(ctx context.Context) ([]string, error)
}

// NewClient creates the appropriate client based on config.
func NewClient(cfg *config.Config) Client {
	if cfg.UseLocal {
		return NewOllamaClient(cfg.OllamaHost, cfg.Model)
	}
	return NewDeepSeekClient(cfg.APIKey, cfg.Model, cfg.APIURL)
}
