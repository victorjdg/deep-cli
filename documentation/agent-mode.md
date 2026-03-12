# Agent Mode

## Overview

Agent mode enables the LLM to autonomously use tools to complete tasks that require reading files, exploring directories, editing code, running commands, or searching the web. It is only available in **cloud mode** (DeepSeek API).

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
        ├── For write_file / patch_file / run_command:
        │     └── Auto-accept OFF? → Show confirmation prompt → Wait for y/n
        │
        ├── Execute each tool locally
        ├── Append tool results to message history
        └── Call LLM again with results (loop, max 10 iterations)
```

Each tool execution is shown in the UI as:

```
  tool: list_files({"path": "."})
  tool: read_file({"path": "main.go"})
  tool: run_command({"command": "go build ./..."})
```

The spinner updates at each phase to indicate what is happening:

```
⠋ Agent thinking...        — model processing the prompt
⠋ Reading file...          — read_file executing
⠋ Searching the web...     — web_search executing
⠋ Fetching page content... — fetch_url executing
⠋ Running command...       — run_command executing
⠋ Processing results...    — model processing tool results
```

## Available Tools

### `list_files`

Lists files and directories at a given path.

```json
{ "path": "." }
```

- Appends `/` suffix to directory names
- Returns up to 200 entries
- Constrained to the working directory

---

### `read_file`

Reads the text content of a file.

```json
{ "path": "internal/api/client.go" }
```

- Max file size: 512 KB
- Returns an error for directories or binary files
- Constrained to the working directory

---

### `read_multiple_files`

Reads several files in a single tool call. More efficient than calling `read_file` repeatedly.

```json
{ "paths": ["internal/api/client.go", "internal/api/types.go"] }
```

- Respects the 512 KB per-file limit
- Individual file errors are reported inline without aborting the whole call
- Each file is returned with a `--- path ---` header separator

---

### `write_file`

Writes text content to a file. Creates the file and any missing parent directories. Overwrites the file if it already exists.

```json
{
  "path": "output/result.md",
  "content": "# Result\n\nHello world."
}
```

- Maximum content size: 4 MB
- Creates parent directories automatically
- Cannot overwrite an existing directory
- Constrained to the working directory
- Requires confirmation when auto-accept is OFF
- The UI shows the path and line count: `write_file {"path":"output/result.md","content":"<3 lines>"}`
- An undo entry is pushed to the stack after a successful write (restores previous content or deletes if new)

---

### `patch_file`

Applies a surgical edit to a file by replacing an exact string. More token-efficient than `write_file` for small changes.

```json
{
  "path": "internal/config/config.go",
  "old_string": "fallbackContextSize = 8192",
  "new_string": "fallbackContextSize = 16384"
}
```

- Fails if `old_string` is not found in the file
- Fails if `old_string` appears more than once (ambiguous edit)
- Max file size: 512 KB
- Requires confirmation when auto-accept is OFF — the confirmation prompt shows a coloured diff (red removed, green added) with 2 lines of context
- An undo entry is pushed to the stack after a successful patch

---

### `search_files`

Searches for a regex pattern across all files in a directory tree, returning `file:line: content` matches.

```json
{
  "query": "safePath",
  "path": "internal",
  "case_insensitive": false
}
```

- `path` defaults to `.` (current directory)
- Skips binary files (null byte detection)
- Returns up to 100 matches
- Results are capped with a notice if the limit is reached

---

### `glob`

Finds files matching a glob pattern.

```json
{ "pattern": "*.go", "path": "internal/tui" }
```

- `path` defaults to `.`
- Matches against filename for simple patterns; against the relative path for patterns containing `/`
- Returns up to 100 results

---

### `get_file_info`

Returns metadata about a file or directory.

```json
{ "path": "internal/tools/tools.go" }
```

Returns: path, type (file/directory), size in bytes, last modified timestamp (RFC3339), permissions, and MIME type (by extension).

---

### `web_search`

Searches the web using the currently configured search engine.

```json
{ "query": "golang context cancellation example" }
```

- Returns up to 5 results (title, URL, snippet)
- Results are formatted as plain text for the LLM
- Requires a configured search engine and API key (see [web-search.md](./web-search.md))

---

### `fetch_url`

Fetches the full text content of a web page. Designed to be used after `web_search` to read the complete content of relevant URLs.

```json
{ "url": "https://doc.rust-lang.org/book/" }
```

- Content is truncated to 8 000 characters to avoid saturating the model context
- Raw HTML is downloaded with a 15-second timeout and a 512 KB limit
- **When Tavily is the active search engine**, uses the Tavily `/extract` API for higher quality extraction (falls back to generic fetch on error)
- **For Brave and SearXNG**, uses HTTP GET + HTML cleaning: removes `<script>`, `<style>`, `<nav>`, `<header>`, `<footer>`, decodes HTML entities, and collapses whitespace
- Sets a browser User-Agent to avoid bot-blocking by common sites

Typical usage pattern:
```
web_search("Rust ownership model") → returns 5 URLs with short snippets
fetch_url("https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html") → full chapter text
```

---

### `run_command`

Runs a shell command via `sh -c` and returns the combined stdout+stderr output.

```json
{ "command": "go build ./..." }
```

- **Always requires confirmation** when auto-accept is OFF — a prompt displays the command before execution
- Timeout: 30 seconds
- Output truncated to 32 KB if exceeded
- Returns `(no output)` if the command produces no output
- Exit errors are included in the output (not surfaced as tool errors) so the model can reason about failures

## Auto-Accept Mode

By default, `write_file`, `patch_file`, and `run_command` pause the agent loop and show a confirmation prompt:

```
╭─ Write file? ─────────────────╮
│                               │
│  internal/config/config.go    │
│  replace 12 chars → 18 chars  │
│                               │
│  [y] confirm    [n] cancel    │
╰───────────────────────────────╯
```

Toggle auto-accept with `/auto` or `Ctrl+A`. When ON, all actions execute immediately. The `│ AUTO` indicator appears in the status bar.

## Security

All file tools enforce a working directory boundary via `safePath()`:

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

## Tool Failure Handling

When a tool fails, it is automatically disabled for the rest of the agent session:

1. The error is shown in the viewport: `⚠ Tool 'web_search' failed and has been disabled for this session: ...`
2. The tool is removed from the definitions sent to the model in subsequent iterations
3. If the model somehow tries to call it again, the cached error is returned immediately

Tools with missing configuration (e.g. `web_search` with no API key set) are removed from the definitions **before the first call** to the model, so no extra iterations are wasted.

## Undo Stack

Every successful `write_file` or `patch_file` operation pushes an entry onto an in-memory undo stack in the TUI. Use `/undo` to revert the last edit, one step at a time.

- **New file**: `/undo` deletes the file
- **Overwritten file**: `/undo` restores the previous content
- The stack is LIFO — multiple `/undo` calls walk back through edits in reverse order
- The stack is session-scoped and lost on exit
- `run_command` is not tracked (shell side effects are not reversible)

## Agent Trace Panel

Press `Ctrl+T` at any time to open the agent trace panel, which shows all tool calls made during the current session along with their results. Useful for debugging unexpected agent behaviour.

Entries accumulate in the background even when the panel is closed. See [tui-components.md](./tui-components.md) for the full widget description.

## Limitations

- Not available in local (Ollama) mode
- Maximum 10 iterations per agent run to prevent infinite loops
- `web_search` requires an external API key or a running SearXNG instance
- `run_command` uses a 30-second timeout; long-running processes will be killed
