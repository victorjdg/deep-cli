# Web Search

## Overview

The `web_search` agent tool allows the LLM to search the web during a conversation. Three search engines are supported, switchable at runtime via the `/search` command.

## Supported Engines

| Engine    | Type         | Required env var          |
|-----------|--------------|---------------------------|
| `tavily`  | Cloud API    | `TAVILY_API_KEY`          |
| `brave`   | Cloud API    | `BRAVE_SEARCH_API_KEY`    |
| `searxng` | Self-hosted  | `SEARXNG_HOST`            |

**Default:** `tavily`

## Switching Engines

Use the `/search` slash command in the interactive REPL:

```
/search              → Shows current engine and available options
/search brave        → Switch to Brave Search
/search searxng      → Switch to SearXNG
/search tavily       → Switch back to Tavily (default)
```

After switching, the app shows which environment variable is required for that engine.

Autocomplete is available: typing `/search ` and pressing Tab shows all available engines.

## Engine Details

### Tavily

- **Endpoint:** `https://api.tavily.com/search`
- **Auth:** `TAVILY_API_KEY` in request body
- **Response fields used:** title, url, content
- **Timeout:** 15 seconds

Sign up at [https://tavily.com](https://tavily.com) for an API key.

### Brave Search

- **Endpoint:** `https://api.search.brave.com/res/v1/web/search`
- **Auth:** `BRAVE_SEARCH_API_KEY` as `X-Subscription-Token` header
- **Response fields used:** title, url, description
- **Timeout:** 15 seconds

Sign up at [https://brave.com/search/api](https://brave.com/search/api) for an API key.

### SearXNG

- **Endpoint:** `{SEARXNG_HOST}/search?format=json`
- **Auth:** None (self-hosted)
- **Response fields used:** title, url, content
- **Timeout:** 15 seconds

Self-host a SearXNG instance and set `SEARXNG_HOST` to its base URL (e.g., `http://localhost:8080`).

## Result Format

Search results are formatted as plain text before being passed to the LLM:

```
Search results for: "golang context cancellation"

1. Understanding context.Context in Go
   https://pkg.go.dev/context
   The context package defines the Context type, which carries deadlines...

2. ...
```

Up to 5 results are returned per query.

## Architecture

The search system is structured around the `search.Engine` interface:

```go
type Engine interface {
    Name() string
    Search(ctx context.Context, query string, maxResults int) ([]Result, error)
}
```

A thread-safe `Manager` holds the currently active engine and allows switching via `SetEngine(name)`. The manager is initialized once in `newModel()` and wired to both the agent tools layer and the `/search` slash command handler.

```
newModel()
  └── search.NewManager()       → defaults to Tavily
       ├── tools.SetSearchManager()     → used by web_search tool
       └── SetSlashSearchManager()      → used by /search command
```
