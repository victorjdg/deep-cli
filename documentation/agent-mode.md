# Agent Mode

## Overview

Agent mode enables the LLM to autonomously use tools to complete tasks that require reading files, exploring directories, or searching the web. It is only available in **cloud mode** (DeepSeek API).

Agent mode is **enabled by default** when using cloud mode. Toggle it with `/agent`.

## How It Works

When agent mode is active, the app replaces the normal streaming flow with a tool-calling loop:

```
User sends message
  │
  ▼
LLM called with tools definition
  │
  ├── No tool calls → Final response → Done
  │
  └── Tool calls returned
        │
        ├── Execute each tool locally
        ├── Append tool results to message history
        └── Call LLM again with results (loop, max 10 iterations)
```

Each tool execution is shown in the UI as:

```
  tool: list_files({"path": "."})
  tool: read_file({"path": "main.go"})
```

## Available Tools

### `list_files`

Lists files and directories at a given path.

```json
{
  "path": "."
}
```

- Appends `/` suffix to directory names
- Returns up to 200 entries
- Constrained to the working directory

### `read_file`

Reads the text content of a file.

```json
{
  "path": "internal/api/client.go"
}
```

- Max file size: 512 KB
- Returns an error for directories or binary files
- Constrained to the working directory

### `write_file`

Writes text content to a file. Creates the file and any missing parent directories. Overwrites the file if it already exists.

```json
{
  "path": "output/result.md",
  "content": "# Result\n\nHello world."
}
```

- Maximum content size: 4 MB
- Creates parent directories automatically (`os.MkdirAll`)
- Cannot overwrite an existing directory
- Constrained to the working directory
- The UI shows `[write]` next to the tool indicator and truncates the content to `<N lines>` to avoid flooding the viewport

### `web_search`

Searches the web using the currently configured search engine.

```json
{
  "query": "golang context cancellation example"
}
```

- Returns up to 5 results (title, URL, snippet)
- Results are formatted as plain text for the LLM
- Requires a configured search engine and API key (see [web-search.md](./web-search.md))

## Security

All file tools enforce a working directory boundary. The `safePath()` function resolves any given path and checks that it does not escape the working directory:

```go
func safePath(path, workDir string) (string, error) {
    abs := filepath.Clean(filepath.Join(workDir, path))
    rel, _ := filepath.Rel(workDir, abs)
    if strings.HasPrefix(rel, "..") {
        return "", errors.New("access denied: path is outside working directory")
    }
    return abs, nil
}
```

Absolute paths are also checked against the working directory boundary.

## Token Usage

The agent loop accumulates token usage across all iterations (including tool result messages). The final total is added to the session's token count and reflected in `/cost`.

## Limitations

- Not available in local (Ollama) mode — Ollama's `CompleteWithTools` falls back to a regular `Complete` call
- Maximum 10 iterations per agent run to prevent infinite loops
- `web_search` requires an external API key or a running SearXNG instance
