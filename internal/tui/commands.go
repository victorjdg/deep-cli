package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/victorjdg/deep-cli/internal/api"
	"github.com/victorjdg/deep-cli/internal/tools"
)

// Messages for the BubbleTea event loop.

type streamStartMsg struct {
	chunks <-chan api.StreamChunk
}

type streamChunkMsg struct {
	content string
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

// listenForChunk reads the next chunk from the channel.
func listenForChunk(ch <-chan api.StreamChunk) tea.Cmd {
	return func() tea.Msg {
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
		return streamChunkMsg{content: chunk.Content}
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
	name string
	args string
}

type agentConfirmRequest struct {
	kind    confirmKind
	title   string
	detail  string
	replyCh chan bool
}

// agentConfirmMsg is sent to the TUI when an action needs user approval.
type agentConfirmMsg struct {
	kind    confirmKind
	title   string
	detail  string
	replyCh chan bool
}

type agentEvent struct {
	done           bool
	content        string
	usage          api.TokenUsage
	err            error
	tool           *agentToolUseMsg   // non-nil when a tool is being called
	confirmRequest *agentConfirmRequest // non-nil when run_command needs approval
}

const maxAgentIterations = 10

func runAgentLoop(client api.Client, messages []api.Message, workDir string, autoAccept bool) (<-chan agentEvent, tea.Cmd) {
	ch := make(chan agentEvent)
	cmd := func() tea.Msg {
		defer close(ch)
		msgs := make([]api.Message, len(messages))
		copy(msgs, messages)

		defs := tools.Definitions()
		var totalUsage api.TokenUsage

		for i := 0; i < maxAgentIterations; i++ {
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

				ch <- agentEvent{tool: &agentToolUseMsg{name: tc.Function.Name, args: displayArgs}}

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
						approved := autoAccept || requestConfirm(ch, confirmKindCommand, cmdArgs.Command, "")
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
						detail := fmt.Sprintf("%d lines", strings.Count(fileArgs.Content, "\n")+1)
						approved := autoAccept || requestConfirm(ch, confirmKindEdit, fileArgs.Path, detail)
						if !approved {
							result = "User declined the file write."
						} else {
							result, execErr = tools.Execute(tc.Function.Name, tc.Function.Arguments, workDir)
							if execErr != nil {
								result = fmt.Sprintf("Error: %s", execErr)
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
						detail := fmt.Sprintf("replace %d chars → %d chars", len(patchArgs.OldString), len(patchArgs.NewString))
						approved := autoAccept || requestConfirm(ch, confirmKindEdit, patchArgs.Path, detail)
						if !approved {
							result = "User declined the file edit."
						} else {
							result, execErr = tools.Execute(tc.Function.Name, tc.Function.Arguments, workDir)
							if execErr != nil {
								result = fmt.Sprintf("Error: %s", execErr)
							}
						}
					}

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

		ch <- agentEvent{err: fmt.Errorf("agent reached maximum iteration limit (%d)", maxAgentIterations)}
		return nil
	}
	return ch, cmd
}

// requestConfirm sends a confirmation request through the agent channel and blocks
// until the TUI responds. Returns true if the user approved.
func requestConfirm(ch chan<- agentEvent, kind confirmKind, title, detail string) bool {
	replyCh := make(chan bool, 1)
	ch <- agentEvent{confirmRequest: &agentConfirmRequest{
		kind:    kind,
		title:   title,
		detail:  detail,
		replyCh: replyCh,
	}}
	return <-replyCh
}

// listenForAgentEvent reads the next event from the agent channel.
func listenForAgentEvent(ch <-chan agentEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		if ev.err != nil {
			return agentErrMsg{err: ev.err}
		}
		if ev.confirmRequest != nil {
			return agentConfirmMsg{
				kind:    ev.confirmRequest.kind,
				title:   ev.confirmRequest.title,
				detail:  ev.confirmRequest.detail,
				replyCh: ev.confirmRequest.replyCh,
			}
		}
		if ev.tool != nil {
			return *ev.tool
		}
		if ev.done {
			return agentDoneMsg{content: ev.content, usage: ev.usage}
		}
		return nil
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
