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

| Operation        | Cmd                      | Msg                                               |
|------------------|--------------------------|---------------------------------------------------|
| Stream response  | `startStream()`          | `streamChunkMsg`, `streamDoneMsg`, `streamErrMsg` |
| Agent loop       | `runAgentLoop()`         | `agentToolUseMsg`, `agentSpinnerMsg`, `agentTraceMsg`, `agentConfirmMsg`, `agentWarnMsg`, `agentDoneMsg`, `agentErrMsg` |
| Compact context  | `compactConversation()`  | `compactDoneMsg`                                  |
| Enhance prompt   | `enhancePrompt()`        | `enhanceDoneMsg`                                  |
| Fetch models     | `fetchModels()`          | `modelsListMsg`                                   |
| Check connection | `checkConnection()`      | `connectionCheckMsg`                              |
| Init project     | `initProject()`          | `initDoneMsg`                                     |

### Channel-Based Agent Progress

The agent loop uses a channel to emit progress events so the UI can update in real time without blocking:

```
runAgentLoop() ──► goroutine ──► ch agentEvent
                                  │
                                  ├── agentSpinnerMsg   (thinking / processing results)
                                  ├── agentToolUseMsg   (each tool call, with spinner label)
                                  ├── agentTraceMsg     (tool result, recorded in trace panel)
                                  ├── agentConfirmMsg   (write_file / patch_file / run_command)
                                  ├── agentWarnMsg      (tool disabled after failure)
                                  ├── agentDoneMsg      (final answer)
                                  └── agentErrMsg       (on failure)

listenForAgentEvent(ch) ──► returns next event as tea.Msg
```

### Confirmation Handshake

For destructive operations (`write_file`, `patch_file`, `run_command`), the agent goroutine blocks on a `chan bool` until the TUI responds:

```
Agent goroutine                      BubbleTea event loop
───────────────────────              ────────────────────────────
Sends agentConfirmMsg
  with replyCh (chan bool, buf=1)
Blocks on <-replyCh      ──────────► TUI enters stateConfirm
                                     Renders confirmation prompt
                                     User presses y/n
                         ◄────────── Sends true/false on replyCh
Unblocks, executes or skips
Continues agent loop
```

When `autoAccept` is `true`, `requestConfirm()` is not called and the tool executes immediately.

## Component Hierarchy

```
Model (tui/model.go)
├── inputModel          — Textarea for user input
├── viewportModel       — Scrollable message display
├── statusBarModel      — Two-line bottom status bar
├── spinnerModel        — Loading indicator
├── completionState     — Autocomplete popup
├── modelPicker         — Model selection popup
├── confirmPrompt       — Confirmation widget (edits and commands)
├── tracePanel          — Agent trace popup (Ctrl+T)
└── promptHistory       — Up/down history navigation
```

Each sub-component is a struct with its own `Update(msg) (Self, tea.Cmd)` and `View() string` methods, following BubbleTea conventions.

## Application States

| State           | Description                                              |
|-----------------|----------------------------------------------------------|
| `stateReady`    | Input focused, waiting for user input                    |
| `stateStreaming` | Active stream, agent loop, or long API call running     |
| `stateConfirm`  | Agent paused, awaiting user confirmation for an action   |

## Status Bar

The status bar is two lines tall (`statusHeight = 2` in `resize()`):

```
 cloud │ deepseek-chat │ T:1234 │ AGENT │ Ctx:9%          /help
 Agent: ON (Ctrl+A)   Auto-accept: OFF (/auto or Ctrl+A)   Enhance: OFF (Ctrl+E)
```

Line 1 shows active mode flags and context usage. Line 2 shows static key hints with current ON/OFF state for the three toggleable modes.
