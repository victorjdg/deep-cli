package tui

import (
	"os"
	"path/filepath"
	"strings"
)

// MaxHistorySize is the maximum number of prompts to keep in history.
var MaxHistorySize = 500

type promptHistory struct {
	entries  []string
	cursor   int    // -1 means "not browsing", 0..len-1 indexes from newest
	draft    string // saves the in-progress input when user starts browsing
	filePath string
}

// historyFilePath returns ~/.local/share/deep-cli/history.
func historyFilePath() string {
	base, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, ".local", "share", "deep-cli", "history")
}

func newPromptHistory() *promptHistory {
	h := &promptHistory{
		cursor:   -1,
		filePath: historyFilePath(),
	}
	h.load()
	return h
}

// load reads persisted history from disk (one entry per line, oldest first).
func (h *promptHistory) load() {
	if h.filePath == "" {
		return
	}
	data, err := os.ReadFile(h.filePath)
	if err != nil {
		return // file doesn't exist yet — that's fine
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for _, line := range lines {
		if line != "" {
			h.entries = append(h.entries, line)
		}
	}
	if len(h.entries) > MaxHistorySize {
		h.entries = h.entries[len(h.entries)-MaxHistorySize:]
	}
}

// save writes the current history to disk.
func (h *promptHistory) save() {
	if h.filePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(h.filePath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(h.filePath, []byte(strings.Join(h.entries, "\n")+"\n"), 0o600)
}

// Add stores a new prompt at the end of history and persists it to disk.
func (h *promptHistory) Add(prompt string) {
	if prompt == "" {
		return
	}
	// Avoid consecutive duplicates.
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == prompt {
		return
	}
	h.entries = append(h.entries, prompt)
	if len(h.entries) > MaxHistorySize {
		h.entries = h.entries[len(h.entries)-MaxHistorySize:]
	}
	h.Reset()
	h.save()
}

// Reset exits browsing mode.
func (h *promptHistory) Reset() {
	h.cursor = -1
	h.draft = ""
}

// Browsing returns true if the user is navigating history.
func (h *promptHistory) Browsing() bool {
	return h.cursor >= 0
}

// Up moves to the previous (older) entry. Returns the entry to display,
// and whether the navigation happened.
// currentInput is the current textarea value, saved as draft on first navigation.
func (h *promptHistory) Up(currentInput string) (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}

	if h.cursor == -1 {
		// Start browsing: save current input as draft.
		h.draft = currentInput
		h.cursor = len(h.entries) - 1
	} else if h.cursor > 0 {
		h.cursor--
	} else {
		// Already at oldest entry.
		return h.entries[h.cursor], false
	}

	return h.entries[h.cursor], true
}

// Down moves to the next (newer) entry. Returns the entry to display.
func (h *promptHistory) Down(currentInput string) (string, bool) {
	if h.cursor == -1 {
		return "", false
	}

	if h.cursor < len(h.entries)-1 {
		h.cursor++
		return h.entries[h.cursor], true
	}

	// Past the newest entry: restore the draft.
	h.cursor = -1
	return h.draft, true
}
