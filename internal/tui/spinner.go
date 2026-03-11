package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type spinnerModel struct {
	spinner spinner.Model
	verb    string
	active  bool
}

func newSpinnerModel() spinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	return spinnerModel{
		spinner: s,
		verb:    "Thinking...",
	}
}

func (m *spinnerModel) Start(verb string) tea.Cmd {
	m.active = true
	m.verb = verb
	return m.spinner.Tick
}

func (m *spinnerModel) Stop() {
	m.active = false
}

func (m spinnerModel) Update(msg tea.Msg) (spinnerModel, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m spinnerModel) View() string {
	if !m.active {
		return ""
	}
	return m.spinner.View() + " " + m.verb
}
