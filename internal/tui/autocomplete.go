package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var slashCommands = []commandInfo{
	{name: "/agent", desc: "Toggle agent mode (tool calling)"},
	{name: "/auto", desc: "Toggle auto-accept edits and commands"},
	{name: "/clear", desc: "Clear conversation history"},
	{name: "/compact", desc: "Compact conversation context"},
	{name: "/cost", desc: "Show token usage"},
	{name: "/enhance", desc: "Toggle prompt enhancement"},
	{name: "/exit", desc: "Exit the application"},
	{name: "/file", desc: "Load file(s) into context"},
	{name: "/help", desc: "Show available commands"},
	{name: "/init", desc: "Generate CONTEXT.md for this project"},
	{name: "/model", desc: "Show or change model"},
	{name: "/models", desc: "List available models"},
	{name: "/search", desc: "Show or change search engine"},
}

type commandInfo struct {
	name string
	desc string
}

type completionState struct {
	candidates []string
	descs      []string // parallel descriptions (empty for file paths)
	selected   int
	prefix     string // the text being completed (last word)
	baseLine   string // text before the prefix
	active     bool
	isFile     bool // true when completing file paths
}

func (c *completionState) Reset() {
	c.candidates = nil
	c.descs = nil
	c.selected = 0
	c.prefix = ""
	c.baseLine = ""
	c.active = false
	c.isFile = false
}

// Refresh recomputes candidates based on the current input value.
// Called on every keystroke when the popup should be visible.
func (c *completionState) Refresh(value string) {
	candidates, descs, prefix, isFile := computeCandidates(value)
	if len(candidates) == 0 {
		c.Reset()
		return
	}

	c.candidates = candidates
	c.descs = descs
	c.prefix = prefix
	c.baseLine = value[:len(value)-len(prefix)]
	c.isFile = isFile
	c.active = true

	// Keep selected in bounds.
	if c.selected >= len(c.candidates) {
		c.selected = len(c.candidates) - 1
	}
}

// ShouldShow returns true if we should display the completion popup.
func (c *completionState) ShouldShow() bool {
	return c.active && len(c.candidates) > 0
}

// MoveUp moves selection up.
func (c *completionState) MoveUp() {
	if c.selected > 0 {
		c.selected--
	} else {
		c.selected = len(c.candidates) - 1
	}
}

// MoveDown moves selection down.
func (c *completionState) MoveDown() {
	if c.selected < len(c.candidates)-1 {
		c.selected++
	} else {
		c.selected = 0
	}
}

// Apply returns the new textarea value with the selected candidate applied.
func (c *completionState) Apply() string {
	if !c.active || len(c.candidates) == 0 {
		return ""
	}
	result := c.baseLine + c.candidates[c.selected]
	// Add trailing space for commands (not for file paths ending in /).
	if !c.isFile || !strings.HasSuffix(c.candidates[c.selected], "/") {
		result += " "
	}
	return result
}

// View renders the popup menu.
func (c *completionState) View(width int) string {
	if !c.ShouldShow() {
		return ""
	}

	menuWidth := width - 4
	if menuWidth < 20 {
		menuWidth = 20
	}

	// Limit visible items.
	maxVisible := 8
	visibleCandidates := c.candidates
	visibleDescs := c.descs
	startIdx := 0
	if len(c.candidates) > maxVisible {
		startIdx = c.selected - maxVisible/2
		if startIdx < 0 {
			startIdx = 0
		}
		if startIdx+maxVisible > len(c.candidates) {
			startIdx = len(c.candidates) - maxVisible
		}
		visibleCandidates = c.candidates[startIdx : startIdx+maxVisible]
		if len(c.descs) > 0 {
			visibleDescs = c.descs[startIdx : startIdx+maxVisible]
		}
	}

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("42")).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	var lines []string
	for i, cand := range visibleCandidates {
		realIdx := startIdx + i
		var line string

		if len(visibleDescs) > i && visibleDescs[i] != "" {
			line = padRight(cand, 12) + " " + descStyle.Render(visibleDescs[i])
		} else {
			line = cand
		}

		if realIdx == c.selected {
			// Render the whole line highlighted.
			if len(visibleDescs) > i && visibleDescs[i] != "" {
				line = padRight(cand, 12) + " " + visibleDescs[i]
			}
			lines = append(lines, " "+selectedStyle.Render(" "+line+" "))
		} else {
			lines = append(lines, " "+normalStyle.Render(" "+line+" "))
		}
	}

	content := strings.Join(lines, "\n")

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42")).
		Width(menuWidth)

	return border.Render(content)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func computeCandidates(value string) (candidates []string, descs []string, prefix string, isFile bool) {
	if value == "" {
		return nil, nil, "", false
	}

	words := strings.Fields(value)
	if len(words) == 0 {
		return nil, nil, "", false
	}

	lastWord := words[len(words)-1]
	// If input ends with a space, there's no partial word to complete.
	if strings.HasSuffix(value, " ") {
		lastCmd := strings.ToLower(words[len(words)-1])
		// Special case: after "/file " show directory listing.
		if lastCmd == "/file" {
			files := completeFilePath("")
			return files, nil, "", true
		}
		// Special case: after "/search " show available engines.
		if lastCmd == "/search" {
			names, descs := completeSearchEngine("")
			return names, descs, "", false
		}
		return nil, nil, "", false
	}

	// Case 1: Completing a slash command.
	if strings.HasPrefix(lastWord, "/") && !hasFileCommand(words, lastWord) && !hasSearchCommand(words, lastWord) {
		cmds, descriptions := completeSlashCommand(lastWord)
		return cmds, descriptions, lastWord, false
	}

	// Case 2: Completing a file path after /file.
	if hasFileCommand(words, lastWord) {
		files := completeFilePath(lastWord)
		return files, nil, lastWord, true
	}

	// Case 3: Completing a search engine after /search.
	if hasSearchCommand(words, lastWord) {
		names, descs := completeSearchEngine(lastWord)
		return names, descs, lastWord, false
	}

	return nil, nil, "", false
}

func completeSlashCommand(partial string) ([]string, []string) {
	partial = strings.ToLower(partial)
	var names []string
	var descs []string
	for _, cmd := range slashCommands {
		if strings.HasPrefix(cmd.name, partial) {
			names = append(names, cmd.name)
			descs = append(descs, cmd.desc)
		}
	}
	return names, descs
}

// hasFileCommand checks if the last word is an argument to /file.
func hasFileCommand(words []string, lastWord string) bool {
	for i := len(words) - 2; i >= 0; i-- {
		w := strings.ToLower(words[i])
		if w == "/file" {
			return true
		}
		if strings.HasPrefix(w, "/") {
			return false
		}
	}
	return false
}

var searchEngines = []commandInfo{
	{name: "tavily", desc: "Tavily Search (TAVILY_API_KEY)"},
	{name: "brave", desc: "Brave Search (BRAVE_SEARCH_API_KEY)"},
	{name: "searxng", desc: "SearXNG (SEARXNG_HOST)"},
}

// hasSearchCommand checks if the last word is an argument to /search.
func hasSearchCommand(words []string, lastWord string) bool {
	for i := len(words) - 2; i >= 0; i-- {
		w := strings.ToLower(words[i])
		if w == "/search" {
			return true
		}
		if strings.HasPrefix(w, "/") {
			return false
		}
	}
	return false
}

func completeSearchEngine(partial string) ([]string, []string) {
	partial = strings.ToLower(partial)
	var names []string
	var descs []string
	for _, eng := range searchEngines {
		if strings.HasPrefix(eng.name, partial) {
			names = append(names, eng.name)
			descs = append(descs, eng.desc)
		}
	}
	return names, descs
}

func completeFilePath(partial string) []string {
	expanded := partial
	tildePrefix := false
	if strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			expanded = filepath.Join(home, expanded[2:])
			tildePrefix = true
		}
	}

	dir := filepath.Dir(expanded)
	base := filepath.Base(expanded)

	if partial == "" || strings.HasSuffix(partial, "/") {
		if partial == "" {
			dir = "."
		} else {
			dir = expanded
		}
		base = ""
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var matches []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}

		if base == "" || strings.HasPrefix(strings.ToLower(name), strings.ToLower(base)) {
			fullPath := filepath.Join(dir, name)
			result := fullPath

			if tildePrefix {
				home, _ := os.UserHomeDir()
				result = "~/" + strings.TrimPrefix(fullPath, home+"/")
			} else if !filepath.IsAbs(partial) {
				cwd, err := os.Getwd()
				if err == nil {
					rel, err := filepath.Rel(cwd, fullPath)
					if err == nil {
						result = rel
					}
				}
			}

			if entry.IsDir() {
				result += "/"
			}

			matches = append(matches, result)
		}
	}

	sort.Strings(matches)
	return matches
}
