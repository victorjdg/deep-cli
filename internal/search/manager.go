package search

import (
	"fmt"
	"sync"
)

var engineEnvVars = map[string]string{
	"tavily":  "TAVILY_API_KEY",
	"brave":   "BRAVE_SEARCH_API_KEY",
	"searxng": "SEARXNG_HOST",
}

// Manager holds the currently selected search engine.
type Manager struct {
	mu      sync.RWMutex
	current Engine
	name    string
}

// NewManager creates a manager with Tavily as the default engine.
func NewManager() *Manager {
	return &Manager{
		current: NewTavily(),
		name:    "tavily",
	}
}

// SetEngine switches the active search engine by name.
// Returns a user-facing message indicating the required env var.
func (m *Manager) SetEngine(name string) (string, error) {
	var engine Engine
	switch name {
	case "tavily":
		engine = NewTavily()
	case "brave":
		engine = NewBrave()
	case "searxng":
		engine = NewSearXNG()
	default:
		return "", fmt.Errorf("Unknown search engine: %s. Available: tavily, brave, searxng", name)
	}

	m.mu.Lock()
	m.current = engine
	m.name = name
	m.mu.Unlock()

	envVar := engineEnvVars[name]
	return fmt.Sprintf("Search engine set to %s. Requires %s env var.", name, envVar), nil
}

// Current returns the active search engine.
func (m *Manager) Current() Engine {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// CurrentName returns the name of the active engine.
func (m *Manager) CurrentName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.name
}
