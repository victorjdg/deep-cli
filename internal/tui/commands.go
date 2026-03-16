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
	"sync"
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

func runAgentLoop(client api.Client, messages []api.Message, workDir string, autoAccept bool, searchMgr *search.Manager, maxSubagents int) (<-chan agentEvent, tea.Cmd) {
	// Semaphore to cap parallel subagent API calls.
	sem := make(chan struct{}, maxSubagents)

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

			// Compute display args up front (needed for both phases).
			displayArgsList := make([]string, len(toolCalls))
			for idx, tc := range toolCalls {
				displayArgs := tc.Function.Arguments
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
				displayArgsList[idx] = displayArgs
			}

			// toolResults holds the result string for each tool call (indexed by position).
			toolResults := make([]string, len(toolCalls))

			// Classify tools: parallel (read-only or autoAccept) vs sequential (needs confirmation).
			type parallelWork struct {
				idx int
				tc  api.ToolCall
			}
			var parallelBatch []parallelWork
			var sequentialBatch []parallelWork

			for idx, tc := range toolCalls {
				if !autoAccept && tools.RequiresConfirmation(tc.Function.Name) {
					sequentialBatch = append(sequentialBatch, parallelWork{idx, tc})
				} else {
					parallelBatch = append(parallelBatch, parallelWork{idx, tc})
				}
			}

			// Phase A — run read-only (and all tools when autoAccept) in parallel.
			if len(parallelBatch) > 0 {
				if len(parallelBatch) == 1 {
					pw := parallelBatch[0]
					ch <- agentEvent{
						tool:         &agentToolUseMsg{name: pw.tc.Function.Name, args: displayArgsList[pw.idx]},
						spinnerLabel: spinnerLabelForTool(pw.tc.Function.Name),
					}
				} else {
					ch <- agentEvent{spinnerLabel: fmt.Sprintf("Agent working (%d tools in parallel)...", len(parallelBatch))}
					for _, pw := range parallelBatch {
						ch <- agentEvent{tool: &agentToolUseMsg{name: pw.tc.Function.Name, args: displayArgsList[pw.idx]}}
					}
				}

				var wg sync.WaitGroup
				var failedMu sync.Mutex
				for _, pw := range parallelBatch {
					wg.Add(1)
					go func(idx int, tc api.ToolCall) {
						defer wg.Done()
						displayArgs := displayArgsList[idx]

						var result string
						var execErr error

						switch tc.Function.Name {
						case "write_file":
							// autoAccept must be true to reach here.
							var fileArgs struct {
								Path    string `json:"path"`
								Content string `json:"content"`
							}
							if err := json.Unmarshal([]byte(tc.Function.Arguments), &fileArgs); err != nil {
								result = fmt.Sprintf("Error: invalid arguments: %s", err)
							} else {
								prev, existed := tools.ReadPrevious(fileArgs.Path, workDir)
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

						case "patch_file":
							// autoAccept must be true to reach here.
							var patchArgs struct {
								Path      string `json:"path"`
								OldString string `json:"old_string"`
								NewString string `json:"new_string"`
							}
							if err := json.Unmarshal([]byte(tc.Function.Arguments), &patchArgs); err != nil {
								result = fmt.Sprintf("Error: invalid arguments: %s", err)
							} else {
								prev, existed := tools.ReadPrevious(patchArgs.Path, workDir)
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

						case "run_command":
							// autoAccept must be true to reach here.
							var cmdArgs struct {
								Command string `json:"command"`
							}
							if err := json.Unmarshal([]byte(tc.Function.Arguments), &cmdArgs); err != nil {
								result = fmt.Sprintf("Error: invalid arguments: %s", err)
							} else {
								result, execErr = execRunCommand(cmdArgs.Command)
								if execErr != nil {
									result = fmt.Sprintf("Error: %s", execErr)
								}
							}

						case "delegate_task":
							var taskArgs struct {
								Task    string `json:"task"`
								Context string `json:"context"`
							}
							if err := json.Unmarshal([]byte(tc.Function.Arguments), &taskArgs); err != nil {
								result = fmt.Sprintf("Error: invalid arguments: %s", err)
							} else {
								result = runSubagent(client, taskArgs.Task, taskArgs.Context, workDir, searchMgr, sem)
							}

						default:
							failedMu.Lock()
							cachedErr, failed := failedTools[tc.Function.Name]
							failedMu.Unlock()
							if failed {
								result = cachedErr
							} else {
								result, execErr = tools.Execute(tc.Function.Name, tc.Function.Arguments, workDir)
								if execErr != nil {
									result = fmt.Sprintf("Error: %s", execErr)
									failedMu.Lock()
									failedTools[tc.Function.Name] = result
									failedMu.Unlock()
									ch <- agentEvent{warn: fmt.Sprintf("Tool '%s' failed and has been disabled for this session: %s", tc.Function.Name, execErr)}
								}
							}
						}

						toolResults[idx] = result
						ch <- agentEvent{trace: &agentTraceMsg{
							tool:   tc.Function.Name,
							args:   displayArgs,
							result: result,
						}}
					}(pw.idx, pw.tc)
				}
				wg.Wait()

				// Disable failed tools from defs after all goroutines complete.
				failedMu.Lock()
				for name := range failedTools {
					defs = removeTool(defs, name)
				}
				failedMu.Unlock()
			}

			// Phase B — sequential confirmation for write/patch/command tools (autoAccept=false).
			for _, pw := range sequentialBatch {
				idx, tc := pw.idx, pw.tc
				displayArgs := displayArgsList[idx]

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
						approved := requestConfirm(ch, confirmKindCommand, cmdArgs.Command, "", nil)
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
						approved := requestConfirm(ch, confirmKindEdit, fileArgs.Path, detail, nil)
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
						approved := requestConfirm(ch, confirmKindEdit, patchArgs.Path, detail, diff)
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
				}

				toolResults[idx] = result
				ch <- agentEvent{trace: &agentTraceMsg{
					tool:   tc.Function.Name,
					args:   displayArgs,
					result: result,
				}}
			}

			// Phase C — append all results in original tool call order.
			for idx, tc := range toolCalls {
				msgs = append(msgs, api.Message{
					Role:       api.RoleTool,
					Content:    toolResults[idx],
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
	case "delegate_task":
		return "Subagent working..."
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

// runSubagent executes a delegated task in a subagent with its own tool loop.
// It acquires a slot from sem to cap parallel concurrency, then runs up to
// maxAgentIterations of CompleteWithTools, returning the final text result.
func runSubagent(client api.Client, task, extraContext, workDir string, searchMgr *search.Manager, sem chan struct{}) string {
	// Acquire semaphore slot.
	sem <- struct{}{}
	defer func() { <-sem }()

	systemPrompt := "You are a focused subagent. Complete the assigned task using the available tools and return a clear, concise result. Do not ask for clarification — work with what you have."

	var userContent string
	if extraContext != "" {
		userContent = fmt.Sprintf("Context:\n%s\n\nTask:\n%s", extraContext, task)
	} else {
		userContent = fmt.Sprintf("Task:\n%s", task)
	}

	msgs := []api.Message{
		{Role: api.RoleSystem, Content: systemPrompt},
		{Role: api.RoleUser, Content: userContent},
	}

	defs := tools.Definitions()
	// Subagents cannot delegate further — remove delegate_task to prevent recursion.
	defs = removeTool(defs, "delegate_task")
	if searchMgr == nil || !searchMgr.IsConfigured() {
		defs = removeTool(defs, "web_search")
	}

	for i := 0; i < maxAgentIterations; i++ {
		content, toolCalls, _, err := client.CompleteWithTools(context.Background(), msgs, defs)
		if err != nil {
			return fmt.Sprintf("Subagent error: %s", err)
		}

		if len(toolCalls) == 0 {
			return content
		}

		msgs = append(msgs, api.Message{
			Role:      api.RoleAssistant,
			ToolCalls: toolCalls,
		})

		for _, tc := range toolCalls {
			var result string
			var execErr error

			// Subagents never run destructive tools — treat them as read-only.
			switch tc.Function.Name {
			case "write_file", "patch_file", "run_command":
				result = fmt.Sprintf("Error: subagents are not permitted to run '%s'", tc.Function.Name)
			default:
				result, execErr = tools.Execute(tc.Function.Name, tc.Function.Arguments, workDir)
				if execErr != nil {
					result = fmt.Sprintf("Error: %s", execErr)
				}
			}

			msgs = append(msgs, api.Message{
				Role:       api.RoleTool,
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	return fmt.Sprintf("Subagent reached iteration limit (%d) without a final answer.", maxAgentIterations)
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
