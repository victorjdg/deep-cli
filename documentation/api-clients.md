# API Client

## Client Interface

The DeepSeek client implements the interface defined in `internal/api/client.go`:

```go
type Client interface {
    Complete(ctx context.Context, messages []Message) (string, TokenUsage, error)
    CompleteWithTools(ctx context.Context, messages []Message, tools []ToolDefinition) (string, []ToolCall, TokenUsage, error)
    Stream(ctx context.Context, messages []Message) (<-chan StreamChunk, error)
    CheckConnection(ctx context.Context) error
    ListModels(ctx context.Context) ([]string, error)
}
```

## DeepSeek Cloud (`internal/api/deepseek.go`)

| Property     | Value                                           |
|--------------|-------------------------------------------------|
| Endpoint     | `https://api.deepseek.com/chat/completions`     |
| Auth         | `Authorization: Bearer <DEEPSEEK_API_KEY>`      |
| Temperature  | `0.1`                                           |
| Max tokens   | `4096` per response                             |
| Timeout      | 30s (regular), 120s (tool calls), none (stream) |

**Streaming:** Uses Server-Sent Events (SSE). Each `data: {...}` line is parsed as a `StreamChunk`. A `data: [DONE]` line signals the end of the stream.

**Tool calling (`CompleteWithTools`):** Sends the `tools` array in the request body. Parses `tool_calls` from the response choice. Returns both the text content and tool calls so the agent loop can execute them.

**Model listing:** GET `https://api.deepseek.com/models`, returns model IDs.

## Token Tracking

Each call returns a `TokenUsage` struct:

```go
type TokenUsage struct {
    PromptTokens     int
    CompletionTokens int
    TotalTokens      int
}
```

The session accumulates token counts across all calls. The `/cost` command displays cumulative session totals and the context window percentage.

## Message Types

```go
type Message struct {
    Role       string     // "system", "user", "assistant", "tool"
    Content    string
    ToolCalls  []ToolCall // Non-nil when the assistant calls tools
    ToolCallID string     // Set on tool result messages
}
```

Tool call messages follow the OpenAI-compatible format used by DeepSeek.
