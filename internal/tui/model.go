package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/victorjdg/deep-cli/internal/api"
	"github.com/victorjdg/deep-cli/internal/config"
	"github.com/victorjdg/deep-cli/internal/search"
	"github.com/victorjdg/deep-cli/internal/session"
	"github.com/victorjdg/deep-cli/internal/tools"
)

type state int

const (
	stateReady state = iota
	stateStreaming
)

type Model struct {
	cfg       *config.Config
	client    api.Client
	session   *session.Session
	input     inputModel
	viewport  viewportModel
	statusBar statusBarModel
	spinner   spinnerModel
	state     state
	width     int
	height    int

	// Autocompletion
	completion  completionState
	modelPicker modelPicker
	history     *promptHistory

	enhanceActive      bool   // prompt enhancement mode
	agentActive        bool   // tool-calling agent mode
	pendingFileContent string // file content waiting for enhance to complete
	pendingMessage     string // original message waiting for enhance to complete

	agentChan <-chan agentEvent // channel for agent loop progress

	// Streaming state
	streamBuf    *strings.Builder
	streamCancel context.CancelFunc
	streamChunks <-chan api.StreamChunk
}

func newModel(cfg *config.Config) Model {
	client := api.NewClient(cfg)
	sess := session.NewWithContext(cfg.Model, cfg.MaxContextTokens)

	// Initialize search manager and wire it up.
	searchManager := search.NewManager()
	tools.SetSearchManager(searchManager)
	SetSlashSearchManager(searchManager)

	mode := "local"
	if !cfg.UseLocal {
		mode = "cloud"
	}

	return Model{
		cfg:       cfg,
		client:    client,
		session:   sess,
		input:     newInputModel(),
		viewport:  newViewportModel(),
		statusBar: newStatusBarModel(mode, cfg.Model, !cfg.UseLocal),
		spinner:   newSpinnerModel(),
		state:       stateReady,
		agentActive: !cfg.UseLocal,
		streamBuf:   &strings.Builder{},
		history:     newPromptHistory(),
	}
}

func Run(cfg *config.Config) error {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.input.textarea.Focus(),
		checkConnection(m.client),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil

	case connectionCheckMsg:
		if msg.err != nil {
			mode := "local"
			if !m.cfg.UseLocal {
				mode = "cloud"
			}
			welcome := fmt.Sprintf("DeepSeek CLI (%s mode)\nWarning: %s", mode, msg.err)
			m.viewport.AddWelcome(welcome)
		} else {
			mode := "local"
			if !m.cfg.UseLocal {
				mode = "cloud"
			}
			welcome := fmt.Sprintf("DeepSeek CLI (%s mode) │ Model: %s │ Host: %s\nType /help for available commands.", mode, m.cfg.Model, m.cfg.OllamaHost)
			if !m.cfg.UseLocal {
				welcome = fmt.Sprintf("DeepSeek CLI (%s mode) │ Model: %s\nType /help for available commands.", mode, m.cfg.Model)
			}

			// Warn if configured model not found in available models.
			if len(msg.models) > 0 {
				found := false
				for _, model := range msg.models {
					if model == m.cfg.Model {
						found = true
						break
					}
				}
				if !found {
					welcome += fmt.Sprintf("\nWarning: Model '%s' not found. Available: %s", m.cfg.Model, strings.Join(msg.models, ", "))
					welcome += "\nUse /model <name> to switch or /models to list all."
				}
			}
			m.viewport.AddWelcome(welcome)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case streamStartMsg:
		m.streamChunks = msg.chunks
		return m, listenForChunk(msg.chunks)

	case streamChunkMsg:
		m.streamBuf.WriteString(msg.content)
		m.viewport.AddStreamingContent(m.streamBuf.String())
		return m, listenForChunk(m.streamChunks)

	case streamDoneMsg:
		content := m.streamBuf.String()
		m.session.AddAssistant(content)
		m.session.AddTokens(msg.usage)
		m.viewport.FinalizeAssistantMessage(content)
		m.statusBar.SetTokens(m.session.Tokens.TotalTokens)
		m.statusBar.SetContextPct(m.session.ContextPercentage())
		if m.session.IsNearLimit(0.9) {
			m.viewport.AddSlashOutput("⚠ Context window usage above 90%. Consider using /clear to reset the conversation.")
		}
		m.streamBuf.Reset()
		m.streamCancel = nil
		m.streamChunks = nil
		m.state = stateReady
		m.spinner.Stop()
		m.input.Focus()
		return m, nil

	case streamErrMsg:
		m.viewport.AddError(msg.err)
		m.streamBuf.Reset()
		if m.streamCancel != nil {
			m.streamCancel()
		}
		m.streamCancel = nil
		m.streamChunks = nil
		m.state = stateReady
		m.spinner.Stop()
		m.input.Focus()
		return m, nil

	case enhanceDoneMsg:
		m.spinner.Stop()
		userMsg := msg.enhanced
		if msg.err != nil || userMsg == "" {
			m.viewport.AddSlashOutput("Enhance failed, using original prompt.")
			userMsg = m.pendingMessage
		}
		var prompt string
		if m.pendingFileContent != "" {
			prompt = m.pendingFileContent + "\n\n" + userMsg
		} else {
			prompt = userMsg
		}
		m.pendingFileContent = ""
		m.pendingMessage = ""
		m.session.AddUser(prompt)
		m.session.AddTokens(msg.usage)
		m.statusBar.SetTokens(m.session.Tokens.TotalTokens)
		m.statusBar.SetContextPct(m.session.ContextPercentage())

		ctx, cancel := context.WithCancel(context.Background())
		m.streamCancel = cancel
		spinnerCmd := m.spinner.Start("Thinking...")
		return m, tea.Batch(spinnerCmd, startStream(m.client, m.session.Messages, ctx))

	case agentToolUseMsg:
		m.spinner.Stop()
		label := fmt.Sprintf("  tool: %s(%s)", msg.name, msg.args)
		if msg.name == "write_file" {
			label += "  [write]"
		}
		m.viewport.AddSlashOutput(label)
		spinnerCmd := m.spinner.Start("Agent working...")
		return m, tea.Batch(spinnerCmd, listenForAgentEvent(m.agentChan))

	case agentDoneMsg:
		m.spinner.Stop()
		m.agentChan = nil
		content := msg.content
		m.session.AddAssistant(content)
		m.session.AddTokens(msg.usage)
		m.viewport.FinalizeAssistantMessage(content)
		m.statusBar.SetTokens(m.session.Tokens.TotalTokens)
		m.statusBar.SetContextPct(m.session.ContextPercentage())
		m.state = stateReady
		m.input.Focus()
		return m, nil

	case agentErrMsg:
		m.spinner.Stop()
		m.agentChan = nil
		m.viewport.AddError(msg.err)
		m.state = stateReady
		m.input.Focus()
		return m, nil

	case compactDoneMsg:
		m.spinner.Stop()
		if msg.err != nil {
			m.viewport.AddError(fmt.Errorf("compact failed: %w", msg.err))
			m.state = stateReady
			m.input.Focus()
			return m, nil
		}
		m.session.Clear()
		m.viewport.Clear()
		m.session.AddUser("Context from previous conversation:\n\n" + msg.summary)
		m.session.AddTokens(msg.usage)
		m.statusBar.SetTokens(m.session.Tokens.TotalTokens)
		m.statusBar.SetContextPct(m.session.ContextPercentage())
		m.viewport.AddSlashOutput("Conversation compacted successfully.")
		m.state = stateReady
		m.input.Focus()
		return m, nil

	case modelsListMsg:
		m.spinner.Stop()
		if msg.err != nil {
			m.viewport.AddError(fmt.Errorf("failed to list models: %w", msg.err))
		} else if len(msg.models) == 0 {
			m.viewport.AddSlashOutput("No models available.")
		} else {
			m.modelPicker.Open(msg.models, m.cfg.Model, m.width)
			m.input.Blur()
		}
		return m, nil
	}

	// Update sub-models.
	if m.state == stateReady {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	if m.spinner.active {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch {
	case key == "ctrl+d":
		return m, tea.Quit

	case key == "ctrl+c":
		if m.completion.active {
			m.completion.Reset()
			m.statusBar.SetHint("")
			return m, nil
		}
		if m.state == stateStreaming && m.streamCancel != nil {
			m.streamCancel()
			m.viewport.AddSlashOutput("Stream cancelled.")
			content := m.streamBuf.String()
			if content != "" {
				m.session.AddAssistant(content)
				m.viewport.FinalizeAssistantMessage(content)
			}
			m.streamBuf.Reset()
			m.streamCancel = nil
			m.streamChunks = nil
			m.state = stateReady
			m.spinner.Stop()
			m.input.Focus()
			return m, nil
		}
		return m, tea.Quit

	case key == "ctrl+e":
		m.enhanceActive = !m.enhanceActive
		m.statusBar.SetEnhance(m.enhanceActive)
		if m.enhanceActive {
			m.viewport.AddSlashOutput("Prompt enhancement ON. Prompts will be improved before sending (uses extra tokens).")
		} else {
			m.viewport.AddSlashOutput("Prompt enhancement OFF.")
		}
		return m, nil

	case key == "ctrl+l":
		m.viewport.Clear()
		return m, nil

	case key == "escape":
		if m.modelPicker.active {
			m.modelPicker.Close()
			m.input.Focus()
			return m, nil
		}
		if m.completion.active {
			m.completion.Reset()
			m.statusBar.SetHint("")
			return m, nil
		}
	}

	// When model picker is active, intercept all keys.
	if m.modelPicker.active {
		switch key {
		case "up":
			m.modelPicker.MoveUp()
		case "down":
			m.modelPicker.MoveDown()
		case "tab":
			m.modelPicker.MoveDown()
		case "enter":
			selected := m.modelPicker.Selected()
			m.modelPicker.Close()
			if selected != "" && selected != m.cfg.Model {
				m.cfg.Model = selected
				m.client = api.NewClient(m.cfg)
				m.statusBar.SetModel(selected)
				m.viewport.AddSlashOutput(fmt.Sprintf("Model changed to: %s", selected))
			}
			m.input.Focus()
		}
		return m, nil
	}

	// When autocomplete popup is active, intercept navigation keys.
	if m.completion.active && m.state == stateReady {
		switch key {
		case "up":
			m.completion.MoveUp()
			return m, nil
		case "down":
			m.completion.MoveDown()
			return m, nil
		case "tab":
			m.completion.MoveDown()
			return m, nil
		case "enter":
			// Apply the selected completion.
			newValue := m.completion.Apply()
			if newValue != "" {
				m.input.textarea.SetValue(newValue)
				m.input.textarea.CursorEnd()
			}
			m.completion.Reset()
			m.statusBar.SetHint("")
			// After completing a dir path, re-trigger completion.
			if strings.HasSuffix(newValue, "/") {
				m.completion.Refresh(newValue)
			}
			return m, nil
		}

		// Any other key: close popup and pass the key through normally.
		m.completion.Reset()
		m.statusBar.SetHint("")
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		// Refresh completion after keystroke.
		m.refreshCompletion()
		return m, cmd
	}

	// Normal mode keys.
	switch key {
	case "enter":
		if m.state != stateReady {
			return m, nil
		}
		return m.handleSubmit()

	case "tab":
		if m.state != stateReady {
			return m, nil
		}
		// Open popup if there are candidates.
		m.completion.Refresh(m.input.Value())
		return m, nil

	case "up":
		if m.state == stateReady {
			entry, ok := m.history.Up(m.input.Value())
			if ok {
				m.input.textarea.SetValue(entry)
				m.input.textarea.CursorEnd()
				return m, nil
			}
		}

	case "down":
		if m.state == stateReady && m.history.Browsing() {
			entry, ok := m.history.Down(m.input.Value())
			if ok {
				m.input.textarea.SetValue(entry)
				m.input.textarea.CursorEnd()
				return m, nil
			}
		}
	}

	// Pass to input, then refresh completion.
	if m.state == stateReady {
		// Any non-arrow key exits history browsing.
		if m.history.Browsing() && key != "up" && key != "down" {
			m.history.Reset()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.refreshCompletion()
		return m, cmd
	}

	return m, nil
}

// refreshCompletion updates the popup based on current input value.
func (m *Model) refreshCompletion() {
	value := m.input.Value()
	// Only auto-show popup when typing a slash command or after /file.
	if strings.Contains(value, "/") {
		m.completion.Refresh(value)
	} else {
		m.completion.Reset()
	}
}

func (m Model) handleSubmit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input.Value())
	if input == "" {
		return m, nil
	}
	m.input.Reset()
	m.history.Add(input)

	// Slash commands.
	if strings.HasPrefix(input, "/") {
		result := handleSlashCommand(input, m.cfg.Model, costInfo{
			PromptTokens:     m.session.Tokens.PromptTokens,
			CompletionTokens: m.session.Tokens.CompletionTokens,
			TotalTokens:      m.session.Tokens.TotalTokens,
			ContextPct:       m.session.ContextPercentage(),
			MaxContextTokens: m.session.MaxContextTokens,
			LastPromptTokens: m.session.LastPromptTokens,
			MessageCount:     len(m.session.Messages),
			EnhanceActive:    m.enhanceActive,
		})

		if result.quit {
			return m, tea.Quit
		}

		if result.clear {
			m.session.Clear()
			m.viewport.Clear()
			m.statusBar.SetTokens(0)
			m.statusBar.SetContextPct(0)
		}

		if result.changeModel != "" {
			m.cfg.Model = result.changeModel
			m.client = api.NewClient(m.cfg)
			m.statusBar.SetModel(result.changeModel)
		}

		if result.fileContent != "" {
			m.session.AddUser(result.fileContent)
		}

		if result.listModels {
			spinnerCmd := m.spinner.Start("Fetching models...")
			return m, tea.Batch(spinnerCmd, fetchModels(m.client))
		}

		if result.compact {
			m.viewport.AddSlashOutput("Compacting conversation...")
			spinnerCmd := m.spinner.Start("Compacting...")
			return m, tea.Batch(spinnerCmd, compactConversation(m.client, m.session.Messages))
		}

		if result.toggleAgent {
			if m.cfg.UseLocal {
				m.viewport.AddSlashOutput("Agent mode is only available in cloud mode.")
				return m, nil
			}
			m.agentActive = !m.agentActive
			m.statusBar.SetAgent(m.agentActive)
			if m.agentActive {
				m.viewport.AddSlashOutput("Agent mode ON. The model can now read files, list directories, write files, and search the web (uses extra tokens).")
			} else {
				m.viewport.AddSlashOutput("Agent mode OFF.")
			}
			return m, nil
		}

		if result.toggleEnhance {
			m.enhanceActive = !m.enhanceActive
			m.statusBar.SetEnhance(m.enhanceActive)
			if m.enhanceActive {
				m.viewport.AddSlashOutput("Prompt enhancement ON. Prompts will be improved before sending (uses extra tokens).")
			} else {
				m.viewport.AddSlashOutput("Prompt enhancement OFF.")
			}
			return m, nil
		}

		if result.output != "" {
			m.viewport.AddSlashOutput(result.output)
		}

		return m, nil
	}

	// Check for inline /file references in the message.
	message, fileContent, fileOutput := extractInlineFiles(input)
	if fileOutput != "" {
		m.viewport.AddSlashOutput(fileOutput)
	}

	if message == "" && fileContent == "" {
		return m, nil
	}

	// Build the prompt: file context first, then the user's question.
	var prompt string
	if fileContent != "" && message != "" {
		prompt = fileContent + "\n\n" + message
	} else if fileContent != "" {
		prompt = fileContent
	} else {
		prompt = message
	}

	// Show the original input in the viewport.
	m.viewport.AddUserMessage(input)
	m.state = stateStreaming
	m.input.Blur()

	// If agent mode is active, use the tool-calling loop.
	if m.agentActive {
		m.session.AddUser(prompt)
		workDir, _ := os.Getwd()
		ch, cmd := runAgentLoop(m.client, m.session.Messages, workDir)
		m.agentChan = ch
		spinnerCmd := m.spinner.Start("Agent thinking...")
		return m, tea.Batch(spinnerCmd, cmd, listenForAgentEvent(ch))
	}

	// If enhance is active, improve the prompt first.
	if m.enhanceActive {
		m.pendingFileContent = fileContent
		m.pendingMessage = message
		spinnerCmd := m.spinner.Start("Enhancing prompt...")
		cmd := enhancePrompt(m.client, message)
		return m, tea.Batch(spinnerCmd, cmd)
	}

	m.session.AddUser(prompt)

	ctx, cancel := context.WithCancel(context.Background())
	m.streamCancel = cancel

	spinnerCmd := m.spinner.Start("Thinking...")

	return m, tea.Batch(
		spinnerCmd,
		startStream(m.client, m.session.Messages, ctx),
	)
}

func (m *Model) resize() {
	statusHeight := 1
	inputHeight := 5 // textarea with border
	// Reserve space for popup if active.
	popupHeight := 0
	if m.completion.ShouldShow() {
		n := len(m.completion.candidates)
		if n > 8 {
			n = 8
		}
		popupHeight = n + 3 // border + padding
	}
	vpHeight := m.height - statusHeight - inputHeight - popupHeight
	if vpHeight < 1 {
		vpHeight = 1
	}

	m.viewport.SetSize(m.width, vpHeight)
	m.input.SetWidth(m.width)
	m.statusBar.SetWidth(m.width)
}

func (m Model) View() string {
	vpView := m.viewport.View()

	var midSection string
	if m.state == stateStreaming && m.spinner.active {
		midSection = m.spinner.View()
	} else {
		midSection = m.input.View()
	}

	// Render model picker or completion popup above the input area.
	if pickerView := m.modelPicker.View(); pickerView != "" {
		midSection = pickerView + "\n" + midSection
	} else if popupView := m.completion.View(m.width); popupView != "" {
		midSection = popupView + "\n" + midSection
	}

	statusView := m.statusBar.View()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		vpView,
		midSection,
		statusView,
	)
}
