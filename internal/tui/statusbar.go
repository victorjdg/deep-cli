package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

type statusBarModel struct {
	mode        string
	model       string
	tokens      int
	contextPct  float64
	width       int
	hint        string // completion hint
	enhance     bool   // prompt enhancement active
	agent       bool   // agent mode active
	autoAccept  bool   // auto-accept edits/commands active
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

func (m *statusBarModel) SetAutoAccept(active bool) {
	m.autoAccept = active
}

func onOff(active bool) string {
	if active {
		return "ON"
	}
	return "OFF"
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

	// ── Line 1: mode, model, tokens, active flags, context % ──
	left := fmt.Sprintf(" %s │ %s │ T:%d", m.mode, m.model, m.tokens)
	if m.enhance {
		left += lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render(" │ ENHANCE")
	}
	if m.agent {
		left += lipgloss.NewStyle().Foreground(lipgloss.Color("34")).Render(" │ AGENT")
	}
	if m.autoAccept {
		left += lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true).Render(" │ AUTO")
	}

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

	right1 := " /help "
	gap1 := m.width - lipgloss.Width(left) - lipgloss.Width(ctxStr) - lipgloss.Width(right1)
	if gap1 < 0 {
		gap1 = 0
	}
	line1 := left + ctxStyled + fmt.Sprintf("%*s", gap1, "") + right1

	// ── Line 2: static key hints ──
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	onStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	offStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	renderHint := func(label, state, shortcut string) string {
		stateStyled := offStyle.Render(state)
		if state == "ON" {
			stateStyled = onStyle.Render(state)
		}
		return hintStyle.Render(" "+label+": ") + stateStyled + hintStyle.Render(" ("+shortcut+")")
	}

	line2 := renderHint("Agent", onOff(m.agent), "Ctrl+A") +
		hintStyle.Render("   ") +
		renderHint("Auto-accept", onOff(m.autoAccept), "/auto or Ctrl+A") +
		hintStyle.Render("   ") +
		renderHint("Enhance", onOff(m.enhance), "Ctrl+E")

	line2 = statusBarStyle.Width(m.width).Render(line2)

	return statusBarStyle.Width(m.width).Render(line1) + "\n" + line2
}
