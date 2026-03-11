# Architecture

## Overview

DeepSeek CLI is a terminal application built with Go and the [BubbleTea](https://github.com/charmbracelet/bubbletea) TUI framework. It follows an event-driven model-update-view architecture with a clean separation between the API layer, session management, and the user interface.

```
┌─────────────────────────────────────────────────────────────┐
│                        BubbleTea Loop                        │
│                                                              │
│  ┌───────────┐    tea.Msg     ┌───────────┐   View()        │
│  │  Update() │ ◄────────────  │  Model    │ ──────────────► │
│  └─────┬─────┘                └─────▲─────┘   Terminal      │
│        │ tea.Cmd                    │                        │
│        ▼                            │                        │
│  ┌─────────────────────┐            │                        │
│  │  Background goroutines           │                        │
│  │  (streaming, agents,│ ──────────┘                        │
│  │   API calls, etc.)  │                                     │
│  └─────────────────────┘                                     │
└─────────────────────────────────────────────────────────────┘
```

## Package Structure

```
internal/
├── api/        Client interface and backend implementations
├── config/     Config struct and resolution logic
├── markdown/   Terminal markdown renderer
├── search/     Web search engine abstraction and implementations
├── session/    Conversation history and token tracking
├── tools/      Agent tool definitions and sandboxed execution
└── tui/        All UI components (model, viewport, input, statusbar, etc.)
```

## Key Patterns

### BubbleTea Model-Update-View

All state lives in `tui.Model`. Each user action or background event produces a `tea.Msg`, which `Update()` handles to produce a new model and zero or more `tea.Cmd` functions. `View()` renders the current state deterministically.

```
User keypress
  → tea.KeyMsg
  → Update() handles it → returns new Model + tea.Cmd
  → tea.Cmd runs in background → returns tea.Msg
  → Update() handles result → returns new Model
  → View() renders terminal
```

### Background Work via tea.Cmd

Long-running operations (API calls, agent loops, streaming) run in goroutines returned as `tea.Cmd`. Each returns a typed `tea.Msg` when done:

| Operation       | Cmd                      | Msg               |
|-----------------|--------------------------|-------------------|
| Stream response | `startStream()`          | `streamChunkMsg`, `streamDoneMsg`, `streamErrMsg` |
| Agent loop      | `runAgentLoop()`         | `agentToolUseMsg`, `agentDoneMsg`, `agentErrMsg` |
| Compact context | `compactConversation()`  | `compactDoneMsg`  |
| Enhance prompt  | `enhancePrompt()`        | `enhanceDoneMsg`  |
| Fetch models    | `fetchModels()`          | `modelsListMsg`   |
| Check connection| `checkConnection()`      | `connectionCheckMsg` |

### Channel-Based Agent Progress

The agent loop uses a channel to emit progress events (tool use, final result, error) so the UI can update in real time without blocking:

```
runAgentLoop() ──► goroutine ──► ch agentEvent
                                  │
                                  ├── agentToolUseMsg (each tool call)
                                  ├── agentDoneMsg (final answer)
                                  └── agentErrMsg (on failure)

listenForAgentEvent(ch) ──► returns next event as tea.Msg
```

## Component Hierarchy

```
Model (tui/model.go)
├── inputModel          — Textarea for user input
├── viewportModel       — Scrollable message display
├── statusBarModel      — Bottom status line
├── spinnerModel        — Loading indicator
├── completionState     — Autocomplete popup
├── modelPicker         — Model selection popup
└── promptHistory       — Up/down history navigation
```

Each sub-component is a struct with its own `Update(msg) (Self, tea.Cmd)` and `View() string` methods, following BubbleTea conventions.

## Application Modes

| Mode      | Condition                         | Notes                          |
|-----------|-----------------------------------|--------------------------------|
| Cloud     | `DEEPSEEK_API_KEY` set            | Agent mode on by default       |
| Local     | No API key or `--local` flag      | Agent mode not available       |
| Streaming | `stateStreaming`                  | Input locked, chunks displayed |
| Ready     | `stateReady`                      | Input focused                  |
