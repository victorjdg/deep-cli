package tui

// MaxHistorySize is the maximum number of prompts to keep in history.
var MaxHistorySize = 20

type promptHistory struct {
	entries []string
	cursor  int    // -1 means "not browsing", 0..len-1 indexes from newest
	draft   string // saves the in-progress input when user starts browsing
}

func newPromptHistory() *promptHistory {
	return &promptHistory{
		cursor: -1,
	}
}

// Add stores a new prompt at the end of history.
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
