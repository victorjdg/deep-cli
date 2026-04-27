# Configuration

## Config Struct

All configuration lives in `internal/config/Config`:

```go
type Config struct {
    APIKey           string
    Model            string
    APIURL           string
    MaxContextTokens int
    MaxSubagents     int
}
```

## Resolution Order

Configuration is resolved in this priority order (highest wins):

1. **CLI flags** (`--api-key`, `--model`, `--max-context`, `--max-subagents`)
2. **Environment variables** (`.env` file is auto-loaded at startup)
3. **Hardcoded defaults**

## Environment Variables

| Variable                | Description                                      | Default                          |
|-------------------------|--------------------------------------------------|----------------------------------|
| `DEEPSEEK_API_KEY`      | API key for DeepSeek (required)                  | —                                |
| `DEEPSEEK_MODEL`        | Model name                                       | `deepseek-v4-pro`                |
| `DEEPSEEK_MAX_CONTEXT`  | Token limit for context window                   | Auto-detected from model         |
| `DEEPSEEK_MAX_SUBAGENTS`| Max parallel subagents for `delegate_task`       | `5`                              |

### Search engine variables (optional)

| Variable                | Description                          |
|-------------------------|--------------------------------------|
| `TAVILY_API_KEY`        | Tavily Search API key                |
| `BRAVE_SEARCH_API_KEY`  | Brave Search API key                 |
| `SEARXNG_HOST`          | Base URL for a SearXNG instance      |

## Context Window Auto-Detection

If `DEEPSEEK_MAX_CONTEXT` is not set, the context size is inferred from the model name using prefix matching:

| Model prefix          | Context tokens |
|-----------------------|----------------|
| `deepseek-v4-pro`     | 1,000,000      |
| `deepseek-v4-flash`   | 1,000,000      |
| Any other             | 8,192          |

## .env File

The app automatically loads a `.env` file from the current working directory at startup (via [godotenv](https://github.com/joho/godotenv)). A template is provided at `.env.example`:

```env
DEEPSEEK_API_KEY=your-api-key-here
# DEEPSEEK_MODEL=deepseek-v4-pro
# DEEPSEEK_MAX_SUBAGENTS=5
```
