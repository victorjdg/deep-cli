package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type confirmKind int

const (
	confirmKindCommand confirmKind = iota
	confirmKindEdit
)

type confirmPrompt struct {
	active  bool
	kind    confirmKind
	title   string // e.g. the command or file path
	detail  string // optional second line (e.g. line count for edits)
	width   int
}

func (c *confirmPrompt) OpenCommand(command string, width int) {
	c.kind = confirmKindCommand
	c.title = command
	c.detail = ""
	c.width = width
	c.active = true
}

func (c *confirmPrompt) OpenEdit(path, detail string, width int) {
	c.kind = confirmKindEdit
	c.title = path
	c.detail = detail
	c.width = width
	c.active = true
}

func (c *confirmPrompt) Close() {
	c.active = false
	c.title = ""
	c.detail = ""
}

func (c *confirmPrompt) View() string {
	if !c.active {
		return ""
	}

	boxWidth := c.width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}

	var headerText string
	var borderColor string
	var contentLine string

	switch c.kind {
	case confirmKindCommand:
		headerText = "⚠ Run command?"
		borderColor = "214"
		contentLine = "$ " + c.title
	case confirmKindEdit:
		headerText = "✎ Write file?"
		borderColor = "33"
		contentLine = c.title
	}

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(borderColor)).
		Bold(true)

	contentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("236")).
		Padding(0, 1)

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Italic(true)

	yStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	nStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Bold(true)

	var lines []string
	lines = append(lines, headerStyle.Render(headerText))
	lines = append(lines, "")
	lines = append(lines, contentStyle.Render(contentLine))
	if c.detail != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("  "+c.detail))
	}
	lines = append(lines, "")
	lines = append(lines, hintStyle.Render("  "+yStyle.Render("[y]")+" confirm    "+nStyle.Render("[n]")+" cancel"))

	content := strings.Join(lines, "\n")

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Width(boxWidth).
		Padding(0, 1)

	return border.Render(content)
}
