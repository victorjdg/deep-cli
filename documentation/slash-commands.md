# Slash Commands

All slash commands are entered directly in the interactive REPL input. Autocomplete is available: press `Tab` after typing `/` to see matching commands.

## Command Reference

### `/help`

Shows all available commands and keyboard shortcuts.

---

### `/file <path> [path2 ...]`

Loads one or more files into the conversation context. File contents are sent as a user message before your next prompt.

```
/file main.go
/file src/api.go src/types.go
/file ~/projects/notes.md
```

Files can also be embedded inline in a message:

```
Explain this /file main.go and suggest improvements
Review /file src/api.go /file src/handlers.go
```

**Limits:**
- Max 512 KB per file
- Text files only (binary files are rejected)
- Supports `~/` path expansion
- Supports 30+ languages for syntax highlighting

---

### `/clear`

Clears the conversation history and resets token counts. The system prompt is kept.

---

### `/compact`

Summarizes the current conversation using the LLM, then replaces the full history with the summary. Useful when the context window is nearly full.

Requires at least one exchange (user + assistant message) to be active. Token usage from the compaction call is tracked in `/cost`.

---

### `/enhance`

Toggles **prompt enhancement mode**. When active, your message is rewritten by the LLM to be clearer and more detailed before being sent. The improvement is transparent — you see only the original message in the viewport, not the rewritten one.

Also available as `Ctrl+E`. Status bar shows `ENHANCE` in yellow when active.

---

### `/agent`

Toggles **agent mode** (tool calling). Only available in cloud mode. When active, the model can use all available tools to autonomously complete tasks.

Status bar shows `AGENT` in green when active. Enabled by default in cloud mode.

See [agent-mode.md](./agent-mode.md) for the full tool reference.

---

### `/auto`

Toggles **auto-accept mode**. Controls whether the agent asks for confirmation before writing files or running commands.

- **OFF** (default) — a confirmation prompt appears for `write_file`, `patch_file`, and `run_command`
- **ON** — all agent actions execute immediately without prompting

Also available as `Ctrl+A`. Status bar shows `AUTO` in amber when active.

---

### `/init`

Analyzes the current project directory and generates a `CONTEXT.md` file documenting its architecture, tech stack, key directories, and build commands.

```
/init
```

How it works:
1. Builds a directory tree (up to 4 levels deep, skipping `node_modules`, `.git`, `vendor`, etc.)
2. Reads key files from the root: `README.md`, `go.mod`, `package.json`, `Makefile`, `Dockerfile`, `.env.example`, and others
3. Sends everything to the model in a single API call (3-minute timeout)
4. Writes the result to `CONTEXT.md` in the current directory
5. Displays a bullet-point summary of findings in the terminal

The generated `CONTEXT.md` can be loaded into context with `/file CONTEXT.md` at the start of future sessions.

---

### `/search [engine]`

Shows or changes the active web search engine.

```
/search              → Show current engine and available options
/search tavily       → Switch to Tavily (requires TAVILY_API_KEY)
/search brave        → Switch to Brave Search (requires BRAVE_SEARCH_API_KEY)
/search searxng      → Switch to SearXNG (requires SEARXNG_HOST)
```

Press `Tab` after `/search ` to see available engines in the autocomplete popup.

---

### `/models`

Fetches the list of available models from the active API endpoint and opens an interactive model picker.

---

### `/model [name]`

Shows the current model, or switches to a different one:

```
/model               → Print current model name
/model deepseek-chat → Switch to deepseek-chat
```

Switching model creates a new API client instance.

---

### `/cost`

Displays token usage for the current session:

```
Session Token Usage:
  Prompt tokens:     12430
  Completion tokens: 3210
  Total tokens:      15640

Context Window:
  Current prompt:    12430 / 128000 tokens
  Usage:             9.7%
```

---

### `/exit`

Exits the application. Equivalent to `Ctrl+D`.

## Autocomplete

The autocomplete popup appears when:

- You type `/` — shows all matching slash commands
- You type `/file ` — shows files and directories in the current directory
- You type `/search ` — shows available search engines

Navigate with `↑`/`↓` or `Tab`, confirm with `Enter`, dismiss with `Escape`.
