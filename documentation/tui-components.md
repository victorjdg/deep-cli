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
    history     *promptHistory

    enhanceActive      bool
    agentActive        bool
    pendingFileContent string
    pendingMessage     string
    agentChan          <-chan agentEvent

    state        state  // stateReady | stateStreaming
    streamBuf    *strings.Builder
    streamCancel context.CancelFunc
    streamChunks <-chan api.StreamChunk
}
```

### Application States

| State            | Description                                |
|------------------|--------------------------------------------|
| `stateReady`     | Input focused, waiting for user input      |
| `stateStreaming` | Active stream or agent loop running        |

## Input (`input.go`)

A `charmbracelet/bubbles/textarea` with a rounded border. Single-line height by default, supporting multi-line input.

- `Focus()` / `Blur()` control keyboard capture
- `Value()` returns current text
- `Reset()` clears the textarea
- `SetWidth(n)` adjusts to terminal width

## Viewport (`viewport.go`)

Scrollable message history. Renders all conversation messages as markdown and supports mouse-wheel scrolling.

**Message types displayed:**

| Method                       | Style              |
|------------------------------|--------------------|
| `AddWelcome(text)`           | Gray, italic       |
| `AddUserMessage(text)`       | Bright, right-padded |
| `AddStreamingContent(text)`  | Live update        |
| `FinalizeAssistantMessage`   | Replaces streaming buffer with final render |
| `AddSlashOutput(text)`       | Yellow/dim         |
| `AddError(err)`              | Red                |

**Streaming optimization:** During streaming, a `cachedPreamble` string stores all completed messages. Only the streaming message is re-rendered on each chunk, avoiding O(n) re-renders.

## Status Bar (`statusbar.go`)

Single-line bar at the bottom of the screen. Shows:

```
cloud │ deepseek-chat │ 15640 tokens │ Ctx: 9.7% │ [ENHANCE] [AGENT]
```

| Indicator | Color  | Condition              |
|-----------|--------|------------------------|
| `ENHANCE` | Yellow | Prompt enhancement ON  |
| `AGENT`   | Green  | Agent mode ON          |

## Spinner (`spinner.go`)

A simple spinner shown during API calls. Displays a label beside the animation:

```
⠋ Thinking...
⠋ Agent working...
⠋ Enhancing prompt...
⠋ Compacting...
```

Uses `charmbracelet/bubbles/spinner` with the `Dot` style.

## Autocomplete (`autocomplete.go`)

Popup menu shown above the input area when typing:

- `/` — slash command completions with descriptions
- `/file ` — filesystem completions (directories/files)
- `/search ` — search engine completions

Navigation: `↑`/`↓`/`Tab` to move, `Enter` to apply, `Escape` to dismiss.

Completing a directory path (ending in `/`) immediately re-triggers completion to show its contents.

## Model Picker (`modelpicker.go`)

Popup shown after `/models` loads the model list. Allows selecting a model with arrow keys and Enter. Closes on Escape without changing model.

## Prompt History (`history.go`)

Stores the last 20 submitted prompts (no consecutive duplicates). Navigate with `↑`/`↓` while the input is empty or matches a history entry.

## Commands (`commands.go`)

Defines all `tea.Cmd` factory functions and their corresponding `tea.Msg` result types. This is where async work is initiated:

```
startStream()           → streamStartMsg → streamChunkMsg... → streamDoneMsg/streamErrMsg
runAgentLoop()          → agentToolUseMsg... → agentDoneMsg/agentErrMsg
enhancePrompt()         → enhanceDoneMsg
compactConversation()   → compactDoneMsg
fetchModels()           → modelsListMsg
checkConnection()       → connectionCheckMsg
```

## Styles (`styles.go`)

Central style definitions using `lipgloss`. Colors are referenced by terminal color index (256-color palette).

## Keymap (`keymap.go`)

Documents the keybindings used throughout the app (informational, not enforced via a keymap struct).
