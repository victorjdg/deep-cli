package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

type statusBarModel struct {
	mode       string
	model      string
	tokens     int
	contextPct float64
	width      int
	hint       string // completion hint
	enhance    bool   // prompt enhancement active
	agent      bool   // agent mode active
}

func newStatusBarModel(mode, model string, agent bool) statusBarModel {
	return statusBarModel{
		mode:  mode,
		model: model,
		agent: agent,
	}
}

func (m *statusBarModel) SetWidth(w int) {
	m.width = w
}

func (m *statusBarModel) SetTokens(n int) {
	m.tokens = n
}

func (m *statusBarModel) SetModel(model string) {
	m.model = model
}

func (m *statusBarModel) SetMode(mode string) {
	m.mode = mode
}

func (m *statusBarModel) SetHint(hint string) {
	m.hint = hint
}

func (m *statusBarModel) SetContextPct(pct float64) {
	m.contextPct = pct
}

func (m *statusBarModel) SetEnhance(active bool) {
	m.enhance = active
}

func (m *statusBarModel) SetAgent(active bool) {
	m.agent = active
}

func (m statusBarModel) View() string {
	if m.hint != "" {
		// Show completion hint as the full status bar.
		hint := " Tab: " + m.hint
		if lipgloss.Width(hint) > m.width {
			hint = hint[:m.width]
		}
		return statusBarStyle.Width(m.width).Render(hint)
	}

	left := fmt.Sprintf(" %s │ %s │ T:%d", m.mode, m.model, m.tokens)
	if m.enhance {
		enhanceTag := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render(" │ ENHANCE")
		left += enhanceTag
	}
	if m.agent {
		agentTag := lipgloss.NewStyle().Foreground(lipgloss.Color("34")).Render(" │ AGENT")
		left += agentTag
	}

	// Add context percentage with color coding.
	ctxStr := fmt.Sprintf(" │ Ctx:%.0f%%", m.contextPct)
	var ctxStyled string
	switch {
	case m.contextPct >= 90:
		ctxStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(ctxStr)
	case m.contextPct >= 75:
		ctxStyled = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render(ctxStr)
	default:
		ctxStyled = ctxStr
	}

	right := " /help "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(ctxStr) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	bar := left + ctxStyled + fmt.Sprintf("%*s", gap, "") + right
	return statusBarStyle.Width(m.width).Render(bar)
}
