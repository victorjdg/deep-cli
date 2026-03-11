# Configuration

## Config Struct

All configuration lives in `internal/config/Config`:

```go
type Config struct {
    APIKey           string
    Model            string
    UseLocal         bool
    OllamaHost       string
    APIURL           string
    MaxContextTokens int
}
```

## Resolution Order

Configuration is resolved in this priority order (highest wins):

1. **CLI flags** (`--api-key`, `--model`, `--local`, `--ollama-host`, `--max-context`)
2. **Environment variables** (`.env` file is auto-loaded at startup)
3. **Hardcoded defaults**

## Environment Variables

| Variable                | Description                                      | Default                          |
|-------------------------|--------------------------------------------------|----------------------------------|
| `DEEPSEEK_API_KEY`      | API key for DeepSeek cloud                       | —                                |
| `DEEPSEEK_MODEL`        | Model name                                       | `deepseek-chat` or `deepseek-coder:6.7b` |
| `DEEPSEEK_USE_LOCAL`    | `"true"` to force Ollama mode                    | Auto (true if no API key)        |
| `OLLAMA_HOST`           | Ollama base URL                                  | `http://localhost:11434`         |
| `DEEPSEEK_MAX_CONTEXT`  | Token limit for context window                   | Auto-detected from model         |

### Search engine variables (optional)

| Variable                | Description                          |
|-------------------------|--------------------------------------|
| `TAVILY_API_KEY`        | Tavily Search API key                |
| `BRAVE_SEARCH_API_KEY`  | Brave Search API key                 |
| `SEARXNG_HOST`          | Base URL for a SearXNG instance      |

## Default Models

| Mode  | Default model           |
|-------|-------------------------|
| Cloud | `deepseek-chat`         |
| Local | `deepseek-coder:6.7b`   |

## Context Window Auto-Detection

If `DEEPSEEK_MAX_CONTEXT` is not set, the context size is inferred from the model name using prefix matching:

| Model prefix         | Context tokens |
|----------------------|----------------|
| `deepseek-chat`      | 128,000        |
| `deepseek-reasoner`  | 128,000        |
| `deepseek-coder`     | 16,384         |
| `deepseek-r1`        | 65,536         |
| Any other            | 8,192          |

## .env File

The app automatically loads a `.env` file from the current working directory at startup (via [godotenv](https://github.com/joho/godotenv)). A template is provided at `.env.example`:

```env
# DEEPSEEK_API_KEY=your-api-key-here
# DEEPSEEK_USE_LOCAL=true
# DEEPSEEK_MODEL=deepseek-chat
# OLLAMA_HOST=http://localhost:11434
```
