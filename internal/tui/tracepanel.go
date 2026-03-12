package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// traceEntry records a single tool call and its result.
type traceEntry struct {
	index  int
	tool   string
	args   string
	result string
}

type tracePanel struct {
	active   bool
	entries  []traceEntry
	viewport viewport.Model
	width    int
	height   int
}

func (t *tracePanel) AddEntry(tool, args, result string) {
	t.entries = append(t.entries, traceEntry{
		index:  len(t.entries) + 1,
		tool:   tool,
		args:   args,
		result: result,
	})
	if t.active {
		t.refresh()
	}
}

func (t *tracePanel) Open(width, height int) {
	t.width = width
	t.height = height
	panelH := height - 6
	if panelH < 5 {
		panelH = 5
	}
	panelW := width - 4
	if panelW < 20 {
		panelW = 20
	}
	t.viewport = viewport.New(panelW-4, panelH-4)
	t.viewport.MouseWheelEnabled = true
	t.active = true
	t.refresh()
}

func (t *tracePanel) Close() {
	t.active = false
}

func (t *tracePanel) Toggle(width, height int) {
	if t.active {
		t.Close()
	} else {
		t.Open(width, height)
	}
}

func (t *tracePanel) Update(msg tea.Msg) tea.Cmd {
	if !t.active {
		return nil
	}
	var cmd tea.Cmd
	t.viewport, cmd = t.viewport.Update(msg)
	return cmd
}

func (t *tracePanel) refresh() {
	if len(t.entries) == 0 {
		t.viewport.SetContent(dimStyle.Render("  No tool calls recorded yet."))
		return
	}

	toolStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	indexStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	resultStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	var sb strings.Builder
	for i, e := range t.entries {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(indexStyle.Render(fmt.Sprintf("[%d] ", e.index)))
		sb.WriteString(toolStyle.Render(e.tool))
		sb.WriteString(dimStyle.Render("(" + truncate(e.args, 80) + ")"))
		sb.WriteString("\n")

		result := e.result
		style := resultStyle
		if strings.HasPrefix(result, "Error:") {
			style = errStyle
		}
		for _, line := range strings.Split(result, "\n") {
			sb.WriteString("    " + style.Render(truncate(line, t.viewport.Width-6)) + "\n")
		}
	}

	t.viewport.SetContent(strings.TrimRight(sb.String(), "\n"))
	t.viewport.GotoBottom()
}

func (t *tracePanel) View() string {
	if !t.active {
		return ""
	}

	panelW := t.width - 4
	if panelW < 20 {
		panelW = 20
	}

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)

	count := fmt.Sprintf("%d tool call", len(t.entries))
	if len(t.entries) != 1 {
		count += "s"
	}

	header := titleStyle.Render("Agent Trace") + "  " +
		dimStyle.Render(count) + "\n" +
		hintStyle.Render("↑↓ scroll · Ctrl+T close")

	content := header + "\n\n" + t.viewport.View()

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("33")).
		Width(panelW).
		Padding(0, 1)

	return border.Render(content)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
