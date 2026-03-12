package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/victorjdg/deep-cli/internal/api"
	"github.com/victorjdg/deep-cli/internal/search"
	"github.com/victorjdg/deep-cli/internal/tools"
)

// Messages for the BubbleTea event loop.

type streamStartMsg struct {
	chunks <-chan api.StreamChunk
}

type streamChunkMsg struct {
	content string
	done    *api.StreamChunk // non-nil when a Done chunk arrived in the same batch
}

type streamDoneMsg struct {
	usage api.TokenUsage
}

type streamErrMsg struct {
	err error
}

type connectionCheckMsg struct {
	err    error
	models []string
}

// startStream initiates streaming and returns the channel.
func startStream(client api.Client, messages []api.Message, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		ch, err := client.Stream(ctx, messages)
		if err != nil {
			return streamErrMsg{err: err}
		}
		return streamStartMsg{chunks: ch}
	}
}

const streamThrottleInterval = 50 * time.Millisecond

// listenForChunk drains the stream channel for up to streamThrottleInterval,
// batching all chunks into a single streamChunkMsg. This reduces viewport
// redraws from ~100/s to ~20/s without affecting streaming latency.
func listenForChunk(ch <-chan api.StreamChunk) tea.Cmd {
	return func() tea.Msg {
		// Block until the first chunk arrives.
		chunk, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		if chunk.Err != nil {
			return streamErrMsg{err: chunk.Err}
		}
		if chunk.Done {
			return streamDoneMsg{usage: chunk.Usage}
		}

		buf := chunk.Content
		deadline := time.NewTimer(streamThrottleInterval)
		defer deadline.Stop()

		// Drain any additional chunks that arrive within the throttle window.
		for {
			select {
			case c, ok := <-ch:
				if !ok {
					// Channel closed mid-batch — flush what we have, next call will return done.
					return streamChunkMsg{content: buf}
				}
				if c.Err != nil {
					return streamErrMsg{err: c.Err}
				}
				if c.Done {
					// Flush accumulated content first; done will be picked up next call.
					// Put the done signal back by returning the chunk with content.
					return streamChunkMsg{content: buf, done: &c}
				}
				buf += c.Content
			case <-deadline.C:
				return streamChunkMsg{content: buf}
			}
		}
	}
}

type modelsListMsg struct {
	models []string
	err    error
}

// fetchModels lists available models from the backend.
func fetchModels(client api.Client) tea.Cmd {
	return func() tea.Msg {
		models, err := client.ListModels(context.Background())
		return modelsListMsg{models: models, err: err}
	}
}

// checkConnection checks if the backend is available.
func checkConnection(client api.Client) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := client.CheckConnection(ctx)
		var models []string
		if err == nil {
			models, _ = client.ListModels(ctx)
		}
		return connectionCheckMsg{err: err, models: models}
	}
}

type compactDoneMsg struct {
	summary string
	usage   api.TokenUsage
	err     error
}

type enhanceDoneMsg struct {
	enhanced string
	usage    api.TokenUsage
	err      error
	// Original submit context to continue with streaming.
	originalInput string
	fileContent   string
}

func enhancePrompt(client api.Client, prompt string) tea.Cmd {
	return func() tea.Msg {
		messages := []api.Message{
			{
				Role: api.RoleUser,
				Content: "You are a prompt engineer. Rewrite the following user prompt to be clearer, more specific, and more effective. " +
					"Keep the same intent and language. Return ONLY the improved prompt, nothing else. No explanations, no preamble.\n\n" +
					"Original prompt:\n" + prompt,
			},
		}
		enhanced, usage, err := client.Complete(context.Background(), messages)
		return enhanceDoneMsg{enhanced: enhanced, usage: usage, err: err}
	}
}

type agentDoneMsg struct {
	content string
	usage   api.TokenUsage
}

type agentErrMsg struct {
	err error
}

// agentToolUseMsg is sent when the agent calls a tool, for display purposes.
type agentToolUseMsg struct {
	name         string
	args         string
	spinnerLabel string
}

// agentSpinnerMsg updates the spinner label without changing other state.
type agentSpinnerMsg struct {
	label string
}

// agentTraceMsg records a completed tool call for the trace panel.
type agentTraceMsg struct {
	tool   string
	args   string
	result string
}

// agentUndoEntry records a reversible file operation for the undo stack.
type agentUndoEntry struct {
	path     string // absolute path
	previous string // content before the edit ("" + wasNew=true means delete on undo)
	wasNew   bool   // true if the file did not exist before
}

type agentConfirmRequest struct {
	kind    confirmKind
	title   string
	detail  string
	diff    []diffLine
	replyCh chan bool
}

// agentConfirmMsg is sent to the TUI when an action needs user approval.
type agentConfirmMsg struct {
	kind    confirmKind
	title   string
	detail  string
	diff    []diffLine
	replyCh chan bool
}

// agentWarnMsg is sent to the TUI to display a warning without stopping the loop.
type agentWarnMsg struct {
	text string
}

type agentEvent struct {
	done           bool
	content        string
	usage          api.TokenUsage
	err            error
	tool           *agentToolUseMsg     // non-nil when a tool is being called
	confirmRequest *agentConfirmRequest // non-nil when run_command needs approval
	warn           string               // non-empty to show a warning in the viewport
	spinnerLabel   string               // non-empty to update the spinner label
	trace          *agentTraceMsg       // non-nil when a tool call completed
	undoEntry      *agentUndoEntry      // non-nil when a file was written/patched
}

const maxAgentIterations = 10

func runAgentLoop(client api.Client, messages []api.Message, workDir string, autoAccept bool, searchMgr *search.Manager) (<-chan agentEvent, tea.Cmd) {
	ch := make(chan agentEvent)
	cmd := func() tea.Msg {
		defer close(ch)
		msgs := make([]api.Message, len(messages))
		copy(msgs, messages)

		defs := tools.Definitions()
		var totalUsage api.TokenUsage
		// failedTools tracks tools that have errored this session.
		// On failure: remove the tool from defs so the model can't call it again,
		// and cache the error in case it somehow tries anyway.
		failedTools := make(map[string]string)

		// Pre-check: remove web_search if no search engine is configured.
		if searchMgr == nil || !searchMgr.IsConfigured() {
			defs = removeTool(defs, "web_search")
		}

		for i := 0; i < maxAgentIterations; i++ {
			if i == 0 {
				ch <- agentEvent{spinnerLabel: "Agent thinking..."}
			} else {
				ch <- agentEvent{spinnerLabel: "Processing results..."}
			}
			content, toolCalls, usage, err := client.CompleteWithTools(context.Background(), msgs, defs)
			if err != nil {
				ch <- agentEvent{err: err}
				return nil
			}
			totalUsage.PromptTokens += usage.PromptTokens
			totalUsage.CompletionTokens += usage.CompletionTokens
			totalUsage.TotalTokens += usage.TotalTokens

			if len(toolCalls) == 0 {
				ch <- agentEvent{done: true, content: content, usage: totalUsage}
				return nil
			}

			// Add assistant message with tool calls.
			msgs = append(msgs, api.Message{
				Role:      api.RoleAssistant,
				ToolCalls: toolCalls,
			})

			// Execute each tool and send progress events.
			for _, tc := range toolCalls {
				displayArgs := tc.Function.Arguments

				// Build a compact display string for write_file and run_command.
				switch tc.Function.Name {
				case "write_file":
					var preview struct {
						Path    string `json:"path"`
						Content string `json:"content"`
					}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &preview); err == nil {
						lines := strings.Count(preview.Content, "\n") + 1
						displayArgs = fmt.Sprintf(`{"path":%q,"content":"<%d lines>"}`, preview.Path, lines)
					}
				case "run_command":
					var preview struct {
						Command string `json:"command"`
					}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &preview); err == nil {
						displayArgs = fmt.Sprintf(`{"command":%q}`, preview.Command)
					}
				}

				ch <- agentEvent{
					tool:         &agentToolUseMsg{name: tc.Function.Name, args: displayArgs},
					spinnerLabel: spinnerLabelForTool(tc.Function.Name),
				}

				var result string
				var execErr error

				switch tc.Function.Name {
				case "run_command":
					var cmdArgs struct {
						Command string `json:"command"`
					}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &cmdArgs); err != nil {
						result = fmt.Sprintf("Error: invalid arguments: %s", err)
					} else {
						approved := autoAccept || requestConfirm(ch, confirmKindCommand, cmdArgs.Command, "", nil)
						if !approved {
							result = "User declined to run the command."
						} else {
							result, execErr = execRunCommand(cmdArgs.Command)
							if execErr != nil {
								result = fmt.Sprintf("Error: %s", execErr)
							}
						}
					}

				case "write_file":
					var fileArgs struct {
						Path    string `json:"path"`
						Content string `json:"content"`
					}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &fileArgs); err != nil {
						result = fmt.Sprintf("Error: invalid arguments: %s", err)
					} else {
						prev, existed := tools.ReadPrevious(fileArgs.Path, workDir)
						detail := fmt.Sprintf("%d lines", strings.Count(fileArgs.Content, "\n")+1)
						approved := autoAccept || requestConfirm(ch, confirmKindEdit, fileArgs.Path, detail, nil)
						if !approved {
							result = "User declined the file write."
						} else {
							result, execErr = tools.Execute(tc.Function.Name, tc.Function.Arguments, workDir)
							if execErr != nil {
								result = fmt.Sprintf("Error: %s", execErr)
							} else {
								absPath, _ := filepath.Abs(filepath.Join(workDir, fileArgs.Path))
								ch <- agentEvent{undoEntry: &agentUndoEntry{
									path:     absPath,
									previous: prev,
									wasNew:   !existed,
								}}
							}
						}
					}

				case "patch_file":
					var patchArgs struct {
						Path      string `json:"path"`
						OldString string `json:"old_string"`
						NewString string `json:"new_string"`
					}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &patchArgs); err != nil {
						result = fmt.Sprintf("Error: invalid arguments: %s", err)
					} else {
						prev, existed := tools.ReadPrevious(patchArgs.Path, workDir)
						diff := buildPatchDiff(patchArgs.OldString, patchArgs.NewString, 2, 20)
						detail := fmt.Sprintf("replace %d chars → %d chars", len(patchArgs.OldString), len(patchArgs.NewString))
						approved := autoAccept || requestConfirm(ch, confirmKindEdit, patchArgs.Path, detail, diff)
						if !approved {
							result = "User declined the file edit."
						} else {
							result, execErr = tools.Execute(tc.Function.Name, tc.Function.Arguments, workDir)
							if execErr != nil {
								result = fmt.Sprintf("Error: %s", execErr)
							} else if existed {
								absPath, _ := filepath.Abs(filepath.Join(workDir, patchArgs.Path))
								ch <- agentEvent{undoEntry: &agentUndoEntry{
									path:     absPath,
									previous: prev,
									wasNew:   false,
								}}
							}
						}
					}

				default:
					// If this tool already failed this session, block the retry immediately.
					if cachedErr, failed := failedTools[tc.Function.Name]; failed {
						result = cachedErr
					} else {
						result, execErr = tools.Execute(tc.Function.Name, tc.Function.Arguments, workDir)
						if execErr != nil {
							result = fmt.Sprintf("Error: %s", execErr)
							failedTools[tc.Function.Name] = result
							// Remove from defs so the model won't call it again next iteration.
							defs = removeTool(defs, tc.Function.Name)
							ch <- agentEvent{warn: fmt.Sprintf("Tool '%s' failed and has been disabled for this session: %s", tc.Function.Name, execErr)}
						}
					}
				}

				ch <- agentEvent{trace: &agentTraceMsg{
					tool:   tc.Function.Name,
					args:   displayArgs,
					result: result,
				}}

				msgs = append(msgs, api.Message{
					Role:       api.RoleTool,
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
		}

		ch <- agentEvent{err: fmt.Errorf("agent reached maximum iteration limit (%d)", maxAgentIterations)}
		return nil
	}
	return ch, cmd
}

// requestConfirm sends a confirmation request through the agent channel and blocks
// until the TUI responds. Returns true if the user approved.
func requestConfirm(ch chan<- agentEvent, kind confirmKind, title, detail string, diff []diffLine) bool {
	replyCh := make(chan bool, 1)
	ch <- agentEvent{confirmRequest: &agentConfirmRequest{
		kind:    kind,
		title:   title,
		detail:  detail,
		diff:    diff,
		replyCh: replyCh,
	}}
	return <-replyCh
}

// listenForAgentEvent reads the next event from the agent channel.
func listenForAgentEvent(ch <-chan agentEvent) tea.Cmd {
	return func() tea.Msg {
		for {
			ev, ok := <-ch
			if !ok {
				// Channel closed without a done/err event — treat as unexpected termination.
				return agentErrMsg{err: fmt.Errorf("agent loop ended unexpectedly")}
			}
			if ev.err != nil {
				return agentErrMsg{err: ev.err}
			}
			if ev.warn != "" {
				return agentWarnMsg{text: ev.warn}
			}
			if ev.trace != nil {
				return agentTraceMsg{tool: ev.trace.tool, args: ev.trace.args, result: ev.trace.result}
			}
			if ev.undoEntry != nil {
				return *ev.undoEntry
			}
			if ev.spinnerLabel != "" && ev.tool == nil && ev.confirmRequest == nil {
				// Pure spinner update — no tool call yet.
				return agentSpinnerMsg{label: ev.spinnerLabel}
			}
			if ev.confirmRequest != nil {
				return agentConfirmMsg{
					kind:    ev.confirmRequest.kind,
					title:   ev.confirmRequest.title,
					detail:  ev.confirmRequest.detail,
					diff:    ev.confirmRequest.diff,
					replyCh: ev.confirmRequest.replyCh,
				}
			}
			if ev.tool != nil {
				return agentToolUseMsg{name: ev.tool.name, args: ev.tool.args, spinnerLabel: ev.spinnerLabel}
			}
			if ev.done {
				return agentDoneMsg{content: ev.content, usage: ev.usage}
			}
			// Empty event — keep reading instead of returning nil to BubbleTea.
		}
	}
}

const maxCommandOutput = 32 * 1024 // 32 KB

func execRunCommand(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	_ = cmd.Run() // intentionally ignore error; output carries the failure info
	output := out.String()
	if len(output) > maxCommandOutput {
		output = output[:maxCommandOutput] + "\n... (output truncated)"
	}
	if output == "" {
		output = "(no output)"
	}
	return output, nil
}

type initDoneMsg struct {
	summary string
	usage   api.TokenUsage
	err     error
}

func initProject(client api.Client, workDir string) tea.Cmd {
	return func() tea.Msg {
		prompt := buildInitPrompt(workDir)
		messages := []api.Message{
			{Role: api.RoleUser, Content: prompt},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		raw, usage, err := client.Complete(ctx, messages)
		if err != nil {
			return initDoneMsg{err: err}
		}
		contextContent, summary := splitInitResponse(raw)
		// Write only the CONTEXT.md portion to disk.
		outPath := workDir + "/CONTEXT.md"
		if writeErr := os.WriteFile(outPath, []byte(contextContent), 0644); writeErr != nil {
			return initDoneMsg{err: fmt.Errorf("generated content but could not write CONTEXT.md: %w", writeErr)}
		}
		return initDoneMsg{summary: summary, usage: usage}
	}
}

type diffLineKind int

const (
	diffContext diffLineKind = iota
	diffAdded
	diffRemoved
)

type diffLine struct {
	kind    diffLineKind
	content string
}

// buildPatchDiff produces a simple line-level diff between oldStr and newStr,
// with up to contextLines of surrounding context. The result is capped at maxDiffLines.
func buildPatchDiff(oldStr, newStr string, contextLines, maxDiffLines int) []diffLine {
	oldLines := strings.Split(oldStr, "\n")
	newLines := strings.Split(newStr, "\n")

	var result []diffLine

	// Find first and last differing line.
	firstDiff, lastDiff := 0, 0
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}
	found := false
	for i := 0; i < maxLen; i++ {
		oldLine := ""
		newLine := ""
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}
		if oldLine != newLine {
			if !found {
				firstDiff = i
				found = true
			}
			lastDiff = i
		}
	}
	if !found {
		return nil
	}

	start := firstDiff - contextLines
	if start < 0 {
		start = 0
	}
	end := lastDiff + contextLines
	if end >= maxLen {
		end = maxLen - 1
	}

	for i := start; i <= end; i++ {
		if len(result) >= maxDiffLines {
			result = append(result, diffLine{kind: diffContext, content: "..."})
			break
		}
		oldLine := ""
		newLine := ""
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}
		if i < firstDiff || i > lastDiff {
			// Context line — show old content (same as new in context range).
			if i < len(oldLines) {
				result = append(result, diffLine{kind: diffContext, content: oldLines[i]})
			}
		} else {
			if oldLine != "" || i < len(oldLines) {
				result = append(result, diffLine{kind: diffRemoved, content: oldLine})
			}
			if newLine != "" || i < len(newLines) {
				result = append(result, diffLine{kind: diffAdded, content: newLine})
			}
		}
	}
	return result
}

// spinnerLabelForTool returns a human-readable label for the spinner while a tool runs.
func spinnerLabelForTool(name string) string {
	switch name {
	case "web_search":
		return "Searching the web..."
	case "fetch_url":
		return "Fetching page content..."
	case "read_file":
		return "Reading file..."
	case "read_multiple_files":
		return "Reading files..."
	case "write_file":
		return "Writing file..."
	case "patch_file":
		return "Patching file..."
	case "list_files":
		return "Listing files..."
	case "search_files":
		return "Searching files..."
	case "glob":
		return "Finding files..."
	case "get_file_info":
		return "Getting file info..."
	case "run_command":
		return "Running command..."
	default:
		return fmt.Sprintf("Calling %s...", name)
	}
}

// removeTool returns a new slice with the named tool removed.
func removeTool(defs []api.ToolDefinition, name string) []api.ToolDefinition {
	result := defs[:0:0]
	for _, d := range defs {
		if d.Function.Name != name {
			result = append(result, d)
		}
	}
	return result
}

func compactConversation(client api.Client, messages []api.Message) tea.Cmd {
	return func() tea.Msg {
		compactMessages := make([]api.Message, len(messages))
		copy(compactMessages, messages)
		compactMessages = append(compactMessages, api.Message{
			Role:    api.RoleUser,
			Content: "Summarize our conversation so far. Keep the most important points, decisions, code snippets, and context needed to continue this conversation. Be concise but preserve critical details.",
		})
		summary, usage, err := client.Complete(context.Background(), compactMessages)
		return compactDoneMsg{summary: summary, usage: usage, err: err}
	}
}
