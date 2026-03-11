package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/victorjdg/deep-cli/internal/markdown"
)

type messageType int

const (
	msgUser messageType = iota
	msgAssistant
	msgError
	msgSlash
	msgWelcome
)

type renderedMessage struct {
	Type    messageType
	Content string
}

type viewportModel struct {
	viewport       viewport.Model
	messages       []renderedMessage
	width          int
	height         int
	cachedPreamble string // rendered messages before the last one (cached during streaming)
	preambleDirty  bool   // true when messages slice changed and preamble needs rebuild
}

func newViewportModel() viewportModel {
	vp := viewport.New(80, 20)
	vp.MouseWheelEnabled = true

	return viewportModel{
		viewport: vp,
		messages: []renderedMessage{},
	}
}

func (m *viewportModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.Width = w
	m.viewport.Height = h
	m.rerender()
}

func (m *viewportModel) AddUserMessage(content string) {
	styled := userPrefixStyle.Render("You: ") + userStyle.Render(content)
	m.messages = append(m.messages, renderedMessage{Type: msgUser, Content: styled})
	m.rerender()
}

func (m *viewportModel) AddAssistantMessage(content string) {
	rendered, err := markdown.Render(content)
	if err != nil {
		rendered = content
	}
	rendered = strings.TrimSpace(rendered)
	styled := assistantPrefixStyle.Render("DeepSeek: ") + "\n" + rendered
	m.messages = append(m.messages, renderedMessage{Type: msgAssistant, Content: styled})
	m.rerender()
}

func (m *viewportModel) AddStreamingContent(content string) {
	styled := assistantPrefixStyle.Render("DeepSeek: ") + "\n" + streamingStyle.Render(content)
	// Replace last message if it's a streaming one, otherwise add new.
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].Type == msgAssistant {
		m.messages[len(m.messages)-1] = renderedMessage{Type: msgAssistant, Content: styled}
	} else {
		m.messages = append(m.messages, renderedMessage{Type: msgAssistant, Content: styled})
		m.preambleDirty = true
	}
	m.rerenderStreaming()
}

func (m *viewportModel) FinalizeAssistantMessage(content string) {
	rendered, err := markdown.Render(content)
	if err != nil {
		rendered = content
	}
	rendered = strings.TrimSpace(rendered)
	styled := assistantPrefixStyle.Render("DeepSeek: ") + "\n" + rendered

	if len(m.messages) > 0 && m.messages[len(m.messages)-1].Type == msgAssistant {
		m.messages[len(m.messages)-1] = renderedMessage{Type: msgAssistant, Content: styled}
	} else {
		m.messages = append(m.messages, renderedMessage{Type: msgAssistant, Content: styled})
	}
	m.rerender()
}

func (m *viewportModel) AddError(err error) {
	styled := errorStyle.Render(fmt.Sprintf("Error: %s", err))
	m.messages = append(m.messages, renderedMessage{Type: msgError, Content: styled})
	m.rerender()
}

func (m *viewportModel) AddSlashOutput(content string) {
	styled := slashOutputStyle.Render(content)
	m.messages = append(m.messages, renderedMessage{Type: msgSlash, Content: styled})
	m.rerender()
}

func (m *viewportModel) AddWelcome(content string) {
	styled := welcomeStyle.Render(content)
	m.messages = append(m.messages, renderedMessage{Type: msgWelcome, Content: styled})
	m.rerender()
}

func (m *viewportModel) Clear() {
	m.messages = []renderedMessage{}
	m.rerender()
}

func (m *viewportModel) rerender() {
	m.preambleDirty = true
	var parts []string
	for _, msg := range m.messages {
		parts = append(parts, msg.Content)
	}
	content := strings.Join(parts, "\n\n")
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

// rerenderStreaming is an optimized rerender for streaming updates.
// It caches the preamble (all messages except the last) and only rebuilds the last message.
func (m *viewportModel) rerenderStreaming() {
	if len(m.messages) == 0 {
		m.viewport.SetContent("")
		m.viewport.GotoBottom()
		return
	}

	if m.preambleDirty || m.cachedPreamble == "" && len(m.messages) > 1 {
		var parts []string
		for _, msg := range m.messages[:len(m.messages)-1] {
			parts = append(parts, msg.Content)
		}
		m.cachedPreamble = strings.Join(parts, "\n\n")
		m.preambleDirty = false
	}

	last := m.messages[len(m.messages)-1].Content
	var content string
	if m.cachedPreamble != "" {
		content = m.cachedPreamble + "\n\n" + last
	} else {
		content = last
	}
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func (m viewportModel) Update(msg tea.Msg) (viewportModel, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m viewportModel) View() string {
	return m.viewport.View()
}
