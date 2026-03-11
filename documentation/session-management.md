# Session Management

## Overview

`internal/session` manages the conversation history, system prompt, and token accounting for the lifetime of an interactive session.

## Session Struct

```go
type Session struct {
    Messages         []api.Message
    Tokens           api.TokenUsage
    MaxContextTokens int
    LastPromptTokens int
    model            string
}
```

## Creating a Session

```go
// With automatic context size detection from model name
sess := session.NewWithContext("deepseek-chat", 0)

// With explicit context size
sess := session.NewWithContext("deepseek-chat", 65536)
```

On creation, the first message in `Messages` is always the system prompt for the given model.

## System Prompts

Each model has a tailored system prompt:

| Model               | Prompt focus                                                 |
|---------------------|--------------------------------------------------------------|
| `deepseek-chat`     | Expert programming assistant, concise, accurate              |
| `deepseek-reasoner` | Expert programming assistant with deep reasoning capabilities |
| Any other model     | Generic expert programming assistant                         |

## Adding Messages

```go
sess.AddUser("Explain goroutines")
sess.AddAssistant("Goroutines are lightweight threads...")
sess.AddTokens(usage)          // Accumulate token counts
```

Tool result messages are appended directly to `sess.Messages` by the agent loop.

## Context Window Tracking

```go
sess.ContextPercentage()       // float64: 0.0–100.0
sess.IsNearLimit(0.9)          // true if > 90% full
sess.EstimateTokens()          // Rough token estimate (chars × 0.3)
```

The app shows a warning in the viewport when context usage exceeds 90%:

```
⚠ Context window usage above 90%. Consider using /clear to reset the conversation.
```

## Clearing vs Compacting

| Action      | What happens                                                          |
|-------------|-----------------------------------------------------------------------|
| `/clear`    | Removes all messages, resets tokens, keeps system prompt              |
| `/compact`  | Replaces all messages with AI-generated summary, keeps system prompt  |

After `/compact`, a user message is injected with the summary so the model has context for future questions:

```
Context from previous conversation:

<LLM-generated summary>
```

## Token Accounting

`session.AddTokens(usage)` accumulates token counts:

```go
Tokens.PromptTokens     += usage.PromptTokens
Tokens.CompletionTokens += usage.CompletionTokens
Tokens.TotalTokens      += usage.TotalTokens
LastPromptTokens         = usage.PromptTokens
```

`LastPromptTokens` stores the prompt size of the most recent API call — this is the value shown as "Current prompt" in `/cost`, which is more useful than the cumulative total for gauging current context usage.
