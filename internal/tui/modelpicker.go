package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type modelPicker struct {
	models   []string
	selected int
	current  string // currently active model (marked with checkmark)
	active   bool
	width    int
}

func (p *modelPicker) Open(models []string, current string, width int) {
	p.models = models
	p.current = current
	p.width = width
	p.active = true

	// Pre-select the current model.
	p.selected = 0
	for i, m := range models {
		if m == current {
			p.selected = i
			break
		}
	}
}

func (p *modelPicker) Close() {
	p.active = false
	p.models = nil
}

func (p *modelPicker) MoveUp() {
	if p.selected > 0 {
		p.selected--
	} else {
		p.selected = len(p.models) - 1
	}
}

func (p *modelPicker) MoveDown() {
	if p.selected < len(p.models)-1 {
		p.selected++
	} else {
		p.selected = 0
	}
}

// Selected returns the model name at the current selection.
func (p *modelPicker) Selected() string {
	if len(p.models) == 0 {
		return ""
	}
	return p.models[p.selected]
}

func (p *modelPicker) View() string {
	if !p.active || len(p.models) == 0 {
		return ""
	}

	menuWidth := p.width - 4
	if menuWidth < 30 {
		menuWidth = 30
	}

	title := " Select a model (↑↓ navigate, Enter select, Esc cancel)"
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Italic(true)

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("42")).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	checkStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42"))

	maxVisible := 10
	startIdx := 0
	visible := p.models
	if len(p.models) > maxVisible {
		startIdx = p.selected - maxVisible/2
		if startIdx < 0 {
			startIdx = 0
		}
		if startIdx+maxVisible > len(p.models) {
			startIdx = len(p.models) - maxVisible
		}
		visible = p.models[startIdx : startIdx+maxVisible]
	}

	var lines []string
	lines = append(lines, titleStyle.Render(title))
	lines = append(lines, "")

	for i, model := range visible {
		realIdx := startIdx + i
		indicator := "  "
		if model == p.current {
			indicator = checkStyle.Render("✓ ")
		}

		if realIdx == p.selected {
			line := indicator + selectedStyle.Render(" "+model+" ")
			lines = append(lines, " "+line)
		} else {
			line := indicator + normalStyle.Render(" "+model)
			lines = append(lines, " "+line)
		}
	}

	if len(p.models) > maxVisible {
		lines = append(lines, "")
		scrollInfo := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		lines = append(lines, scrollInfo.Render(
			strings.Repeat(" ", 3)+"↑↓ scroll for more",
		))
	}

	content := strings.Join(lines, "\n")

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("33")).
		Width(menuWidth).
		Padding(0, 1)

	return border.Render(content)
}
