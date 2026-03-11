# Deep-CLI

A terminal AI assistant powered by [DeepSeek](https://deepseek.com), written in Go. Supports both cloud mode (DeepSeek API) and local mode (Ollama), with a rich interactive TUI, agent mode with tool calling, and web search capabilities.

## Features

- **Interactive TUI** with streaming responses and markdown rendering
- **Agent mode** — the model can read files, list directories, and search the web
- **Slash commands** for managing context, models, and settings
- **Prompt enhancement** — automatically rewrites your prompt before sending
- **Context compression** — summarize long conversations to free up token space
- **Dual backend** — DeepSeek cloud API or local Ollama models
- **Autocomplete** for commands, file paths, and search engines
- **Prompt history** with up/down navigation

---

## Installation

### Prerequisites

- Go 1.22 or later
- A [DeepSeek API key](https://platform.deepseek.com) (cloud mode) **or** [Ollama](https://ollama.com) installed (local mode)

### Build from source

```bash
git clone https://github.com/victorjdg/deep-cli
cd deep-cli/go
make build
```

This produces a `deepseek` binary in the current directory.

### Install to PATH

```bash
make install
```

Copies the binary to `$GOPATH/bin` or `/usr/local/bin`.

### Linux builds (amd64 and arm64)

```bash
make build-all   # Produces binaries for Linux amd64 and arm64 in dist/
```

---

## Configuration

Copy `.env.example` to `.env` and fill in your values:

```bash
cp .env.example .env
```

| Environment Variable     | Description                                       | Default                        |
|--------------------------|---------------------------------------------------|--------------------------------|
| `DEEPSEEK_API_KEY`       | DeepSeek API key (required for cloud mode)        | —                              |
| `DEEPSEEK_MODEL`         | Model to use                                      | `deepseek-chat` / `deepseek-coder:6.7b` |
| `DEEPSEEK_USE_LOCAL`     | Set to `true` to force local Ollama mode          | `true` if no API key is set    |
| `OLLAMA_HOST`            | Ollama server URL                                 | `http://localhost:11434`       |
| `DEEPSEEK_MAX_CONTEXT`   | Max context window size in tokens                 | Auto-detected from model       |
| `TAVILY_API_KEY`         | API key for Tavily web search                     | —                              |
| `BRAVE_SEARCH_API_KEY`   | API key for Brave Search                          | —                              |
| `SEARXNG_HOST`           | Base URL for SearXNG instance                     | —                              |

Configuration priority (highest to lowest): **CLI flags → environment variables → defaults**

---

## Usage

### Interactive mode (REPL)

```bash
deepseek [flags]
```

Flags:

| Flag              | Short | Description                          |
|-------------------|-------|--------------------------------------|
| `--api-key`       | `-k`  | DeepSeek API key                     |
| `--model`         | `-m`  | Model name to use                    |
| `--local`         | `-l`  | Force local Ollama mode              |
| `--ollama-host`   |       | Ollama server URL                    |
| `--max-context`   |       | Context window size in tokens        |

### Single-prompt mode

```bash
deepseek chat "Explain what a B-tree is"
```

### Ollama setup helper

```bash
deepseek setup
```

Checks your Ollama installation, starts the service if needed, and downloads a model.

---

## Slash Commands

Type any command starting with `/` in the interactive REPL:

| Command               | Description                                                  |
|-----------------------|--------------------------------------------------------------|
| `/help`               | Show all available commands and keyboard shortcuts           |
| `/file <path> ...`    | Load one or more files into the conversation context         |
| `/clear`              | Clear conversation history                                   |
| `/compact`            | Summarize and compress the conversation (AI-assisted)        |
| `/enhance`            | Toggle prompt enhancement mode                               |
| `/agent`              | Toggle agent mode (tool calling) — cloud mode only           |
| `/search [engine]`    | Show or change the active web search engine                  |
| `/models`             | List all available models from the API                       |
| `/model [name]`       | Show current model or switch to a new one                    |
| `/cost`               | Show token usage and context window percentage               |
| `/exit`               | Exit the application                                         |

### Inline file loading

You can embed file references directly in your messages:

```
Explain this /file main.go
Review these /file src/api.go /file src/types.go and suggest improvements
```

---

## Keyboard Shortcuts

| Shortcut  | Action                                      |
|-----------|---------------------------------------------|
| `Enter`   | Submit message                              |
| `Ctrl+E`  | Toggle prompt enhancement                   |
| `Ctrl+L`  | Clear the screen                            |
| `Ctrl+C`  | Cancel ongoing stream / Quit                |
| `Ctrl+D`  | Quit                                        |
| `Tab`     | Autocomplete command, file path, or engine  |
| `↑ / ↓`  | Navigate prompt history                     |
| `Escape`  | Close autocomplete or model picker          |

---

## Agent Mode

In cloud mode, the model can autonomously use tools to answer questions that require exploring your filesystem or searching the web. Agent mode is **enabled by default** in cloud mode and can be toggled with `/agent`.

### Available tools

| Tool          | Description                                                       |
|---------------|-------------------------------------------------------------------|
| `list_files`  | List files and directories at a given path (max 200 entries)      |
| `read_file`   | Read the contents of a text file (max 512 KB)                     |
| `write_file`  | Write text content to a file, creating it or overwriting it (max 4 MB) |
| `web_search`  | Search the web using the configured search engine (max 5 results) |

The agent runs in a loop of up to 10 iterations: each iteration calls the LLM, executes any requested tools, feeds results back, and repeats until the model produces a final response with no tool calls.

> **Security:** File tools are sandboxed to the current working directory. Path traversal attempts are blocked.

---

## Web Search

The `web_search` tool uses the engine configured via `/search`. Three engines are supported:

| Engine    | Env var required          | Type        |
|-----------|---------------------------|-------------|
| `tavily`  | `TAVILY_API_KEY`          | Cloud API   |
| `brave`   | `BRAVE_SEARCH_API_KEY`    | Cloud API   |
| `searxng` | `SEARXNG_HOST`            | Self-hosted |

Switch engines at runtime:

```
/search brave
/search searxng
```

The engine selection is **not persisted** between sessions; configure your preferred engine via the default in the source or by running `/search <engine>` after starting the app.

---

## Supported Models

### DeepSeek cloud (`--api-key` required)

| Model               | Context    | Use case                                |
|---------------------|------------|-----------------------------------------|
| `deepseek-chat`     | 128K tokens | General purpose, coding, analysis       |
| `deepseek-reasoner` | 128K tokens | Complex reasoning and problem solving   |

### Local via Ollama

Any model available in your Ollama installation. Examples:

```
deepseek-coder:6.7b
deepseek-coder:33b
deepseek-r1:7b
```

Run `deepseek setup` to pull a model automatically, or use `/models` to list available ones.

---

## Project Structure

```
go/
├── main.go                   # Entry point
├── Makefile                  # Build targets
├── .env.example              # Environment variable template
├── cmd/
│   ├── root.go               # Interactive REPL command
│   ├── chat.go               # Single-prompt command
│   └── setup.go              # Ollama setup command
└── internal/
    ├── api/                  # API clients (DeepSeek, Ollama)
    ├── config/               # Configuration loading
    ├── markdown/             # Terminal markdown rendering
    ├── search/               # Web search engines
    ├── session/              # Conversation and token management
    ├── tools/                # Agent tool definitions and execution
    └── tui/                  # BubbleTea TUI components
```

See the [documentation](./documentation/) directory for detailed internals.

---

## Contributing

Contributions are welcome! Whether it's a bug report, a feature request, or a pull request — all input is appreciated.

- **Found a bug?** Open an issue describing what happened and how to reproduce it.
- **Have an idea?** Open an issue to discuss it before implementing, so we can make sure it fits the project direction.
- **Want to contribute code?** Fork the repo, make your changes, and open a pull request. Keep changes focused and include a clear description of what you did and why.

There are no strict rules — just be respectful and constructive. Every contribution, big or small, is valued.

---

## Acknowledgements

This project was inspired by [deepseek-cli](https://github.com/holasoymalva/deepseek-cli) by [@holasoymalva](https://github.com/holasoymalva). Thanks for the original idea and the motivation to build on top of it.

---

## License

MIT
