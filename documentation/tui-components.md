# TUI Components

All UI components live in `internal/tui/`. The main model composes them and delegates rendering and event handling.

## Model (`model.go`)

The root BubbleTea model. Holds all state:

```go
type Model struct {
    cfg         *config.Config
    client      api.Client
    session     *session.Session
    input       inputModel
    viewport    viewportModel
    statusBar   statusBarModel
    spinner     spinnerModel
    completion  completionState
    modelPicker modelPicker
    confirmPrompt confirmPrompt
    tracePanel  tracePanel
    history     *promptHistory

    enhanceActive      bool
    agentActive        bool
    autoAccept         bool
    pendingFileContent string
    pendingMessage     string
    agentChan          <-chan agentEvent
    confirmReplyCh     chan<- bool
    searchManager      *search.Manager
    undoStack          []agentUndoEntry

    state        state  // stateReady | stateStreaming | stateConfirm
    streamBuf    *strings.Builder
    streamCancel context.CancelFunc
    streamChunks <-chan api.StreamChunk
}
```

### Application States

| State            | Description                                             |
|------------------|---------------------------------------------------------|
| `stateReady`     | Input focused, waiting for user input                   |
| `stateStreaming` | Active stream or agent loop running                     |
| `stateConfirm`   | Agent paused, showing confirmation prompt for an action |

## Input (`input.go`)

A `charmbracelet/bubbles/textarea` with a rounded border. Single-line height by default, supporting multi-line input.

- `Focus()` / `Blur()` control keyboard capture
- `Value()` returns current text
- `Reset()` clears the textarea
- `SetWidth(n)` adjusts to terminal width

## Viewport (`viewport.go`)

Scrollable message history. Renders all conversation messages as markdown and supports mouse-wheel scrolling.

**Message types displayed:**

| Method                       | Style                |
|------------------------------|----------------------|
| `AddWelcome(text)`           | Gray, italic         |
| `AddUserMessage(text)`       | Bright, right-padded |
| `AddStreamingContent(text)`  | Live update          |
| `FinalizeAssistantMessage`   | Replaces streaming buffer with final render |
| `AddSlashOutput(text)`       | Yellow/dim           |
| `AddError(err)`              | Red                  |

**Streaming optimization:** During streaming, a `cachedPreamble` string stores all completed messages. Only the streaming message is re-rendered on each chunk, avoiding O(n) re-renders.

## Status Bar (`statusbar.go`)

Two-line bar at the bottom of the screen.

**Line 1** — mode, model, tokens, active flags, context percentage:
```
 cloud │ deepseek-chat │ T:1234 │ AGENT │ AUTO │ Ctx:9%          /help
```

**Line 2** — static key hints showing current state of each toggle:
```
 Agent: ON (Ctrl+A)   Auto-accept: OFF (/auto or Ctrl+A)   Enhance: OFF (Ctrl+E)
```

ON state is rendered in green; OFF state in dim gray.

| Indicator | Color  | Condition              |
|-----------|--------|------------------------|
| `ENHANCE` | Yellow | Prompt enhancement ON  |
| `AGENT`   | Green  | Agent mode ON          |
| `AUTO`    | Amber  | Auto-accept ON         |

When autocomplete is active, the status bar is replaced by a `Tab: <hint>` completion hint.

## Spinner (`spinner.go`)

A simple spinner shown during API calls. Renders only when `m.state == stateStreaming && m.spinner.active`. Displays a label beside the animation:

```
⠋ Thinking...
⠋ Agent thinking...
⠋ Agent working...
⠋ Running command...
⠋ Enhancing prompt...
⠋ Compacting...
⠋ Generating CONTEXT.md...
```

Uses `charmbracelet/bubbles/spinner` with the `Dot` style.

## Confirm Prompt (`confirm.go`)

A popup widget shown when the agent requests a destructive action and auto-accept is OFF. Pauses the agent goroutine via a buffered `chan bool` until the user responds.

Two visual variants:

**Edit confirmation** (blue border, for `write_file` and `patch_file`):
```
╭─ Write file? ──────────────────╮
│                                │
│  internal/config/config.go     │
│  replace 12 chars → 18 chars   │
│                                │
│  [y] confirm    [n] cancel     │
╰────────────────────────────────╯
```

**Command confirmation** (amber border, for `run_command`):
```
╭─ Run command? ─────────────────╮
│                                │
│  $ go test ./...               │
│                                │
│  [y] confirm    [n] cancel     │
╰────────────────────────────────╯
```

Press `y`/`Y` to approve or `n`/`N`/`Escape` to cancel. Either answer unblocks the agent goroutine.

## Trace Panel (`tracepanel.go`)

Popup shown when the user presses `Ctrl+T`. Displays all tool calls made by the agent during the current session, including their arguments and results.

```
╭─ Agent Trace  3 tool calls ────────────────────────────────────╮
│ Agent Trace  3 tool calls                                       │
│ ↑↓ scroll · Ctrl+T close                                       │
│                                                                 │
│ [1] list_files({"path":"."})                                    │
│     main.go                                                     │
│     internal/                                                   │
│                                                                 │
│ [2] web_search({"query":"B-tree"})                              │
│     Error: search not configured                                │
│                                                                 │
│ [3] read_file({"path":"internal/tools/tools.go"})               │
│     package tools...                                            │
╰─────────────────────────────────────────────────────────────────╯
```

- Entries accumulate in the background even when the panel is closed
- Errors are shown in red, normal results in white
- Arguments are truncated to 80 chars to avoid noise
- Result lines are truncated to the panel width
- Supports scroll with `↑`/`↓`
- `Ctrl+T` toggles open/close

## Autocomplete (`autocomplete.go`)

Popup menu shown above the input area when typing:

- `/` — slash command completions with descriptions
- `/file ` — filesystem completions (directories/files)
- `/search ` — search engine completions

Navigation: `↑`/`↓`/`Tab` to move, `Enter` to apply, `Escape` to dismiss.

Completing a directory path (ending in `/`) immediately re-triggers completion to show its contents.

The command list is defined in `slashCommands` at the top of `autocomplete.go` and must be updated whenever a new slash command is added.

## Model Picker (`modelpicker.go`)

Popup shown after `/models` loads the model list. Allows selecting a model with arrow keys and Enter. Closes on Escape without changing model.

## Prompt History (`history.go`)

Stores the last 20 submitted prompts (no consecutive duplicates). Navigate with `↑`/`↓` while the input is empty or matches a history entry.

## Commands (`commands.go`)

Defines all `tea.Cmd` factory functions and their corresponding `tea.Msg` result types. This is where async work is initiated:

```
startStream()           → streamChunkMsg (batched ~50ms)... → streamDoneMsg/streamErrMsg
runAgentLoop()          → agentSpinnerMsg → agentToolUseMsg → agentTraceMsg → agentUndoEntry
                          → agentConfirmMsg? → agentWarnMsg? → agentDoneMsg/agentErrMsg
enhancePrompt()         → enhanceDoneMsg
compactConversation()   → compactDoneMsg
fetchModels()           → modelsListMsg
checkConnection()       → connectionCheckMsg
initProject()           → initDoneMsg
```

Also contains:
- `execRunCommand()` — runs shell commands with a 30-second timeout, captures stdout+stderr
- `buildPatchDiff()` — generates a line-level diff with context lines for `patch_file` confirmations
- `tools.ReadPrevious()` — reads the current file content before a write, used to build undo entries

## Init (`init.go`)

Implements the `/init` project analysis logic:

- `buildProjectTree(root)` — recursive directory tree up to 4 levels, skipping ignored directories
- `collectKeyFiles(root)` — reads key files (`README.md`, `go.mod`, `Makefile`, etc.) up to 32 KB each / 128 KB total
- `buildInitPrompt(root)` — assembles the full prompt including tree, key files, and instructions
- `splitInitResponse(response)` — splits the model response on `---SUMMARY---` into the `CONTEXT.md` content and terminal summary

## Styles (`styles.go`)

Central style definitions using `lipgloss`. Colors are referenced by terminal color index (256-color palette).

| Variable           | Color  | Usage                          |
|--------------------|--------|--------------------------------|
| `userStyle`        | Green  | User message prefix            |
| `assistantPrefixStyle` | Blue | Assistant message prefix     |
| `errorStyle`       | Red    | Error messages                 |
| `slashOutputStyle` | Gray   | Slash command output           |
| `warningStyle`     | Amber  | Warnings and confirm prompt    |
| `dimStyle`         | Gray   | Dim text (hints, separators)   |

## Keymap (`keymap.go`)

Documents keybindings used throughout the app:

| Key       | Action                    |
|-----------|---------------------------|
| `Enter`   | Submit message            |
| `Ctrl+D`  | Quit                      |
| `Ctrl+C`  | Cancel stream / Quit      |
| `Ctrl+L`  | Clear screen              |
| `Ctrl+E`  | Toggle prompt enhancement |
| `Ctrl+A`  | Toggle auto-accept        |
| `Ctrl+T`  | Toggle agent trace panel  |
