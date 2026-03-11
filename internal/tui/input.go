package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type inputModel struct {
	textarea textarea.Model
	width    int
}

func newInputModel() inputModel {
	ta := textarea.New()
	ta.Placeholder = "Type your message... (Enter to send, Ctrl+D to quit)"
	ta.Focus()
	ta.CharLimit = 0
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle().
		BorderForeground(lipgloss.Color("42")).
		BorderStyle(lipgloss.RoundedBorder())
	ta.BlurredStyle.Base = lipgloss.NewStyle().
		BorderForeground(lipgloss.Color("240")).
		BorderStyle(lipgloss.RoundedBorder())

	return inputModel{
		textarea: ta,
	}
}

func (m *inputModel) SetWidth(w int) {
	m.width = w
	m.textarea.SetWidth(w - 2) // account for border
}

func (m *inputModel) Focus() {
	m.textarea.Focus()
}

func (m *inputModel) Blur() {
	m.textarea.Blur()
}

func (m *inputModel) Value() string {
	return m.textarea.Value()
}

func (m *inputModel) Reset() {
	m.textarea.Reset()
}

func (m inputModel) Update(msg tea.Msg) (inputModel, tea.Cmd) {
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m inputModel) View() string {
	return m.textarea.View()
}
