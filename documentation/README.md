# Documentation

Detailed technical reference for DeepSeek CLI internals.

| Document                                        | Contents                                                         |
|-------------------------------------------------|------------------------------------------------------------------|
| [architecture.md](./architecture.md)            | Overall design, package structure, key patterns                  |
| [configuration.md](./configuration.md)          | Config struct, env vars, defaults, context detection             |
| [slash-commands.md](./slash-commands.md)        | All slash commands with behavior details                         |
| [agent-mode.md](./agent-mode.md)                | Tool calling loop, all tools, confirmation flow, auto-accept     |
| [web-search.md](./web-search.md)                | Search engines, setup, engine switching                          |
| [api-clients.md](./api-clients.md)              | DeepSeek and Ollama client implementations                       |
| [session-management.md](./session-management.md)| Conversation history, token tracking, compaction                 |
| [tui-components.md](./tui-components.md)        | All UI components, confirm prompt, init logic, keymap            |
