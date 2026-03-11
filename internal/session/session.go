package session

import (
	"github.com/victorjdg/deep-cli/internal/api"
)

var systemPrompts = map[string]string{
	"deepseek-chat": "You are an expert programming assistant. Help with coding tasks, explain concepts clearly, " +
		"provide correct and idiomatic code examples, debug issues, and follow best practices. " +
		"Be concise and direct. When showing code, use the appropriate language and include only what is necessary.",
	"deepseek-reasoner": "You are an expert programming assistant with deep reasoning capabilities. " +
		"Think step by step through complex problems. Help with coding tasks, architecture decisions, " +
		"debugging, and code review. Provide correct and idiomatic code examples, and follow best practices. " +
		"Be thorough in your analysis but concise in your explanations.",
}

const defaultSystemPrompt = "You are an expert programming assistant. Help with coding tasks, provide clear code examples, and follow best practices."

// SystemPromptForModel returns the system prompt for the given model name.
func SystemPromptForModel(model string) string {
	if prompt, ok := systemPrompts[model]; ok {
		return prompt
	}
	return defaultSystemPrompt
}

type Session struct {
	Messages         []api.Message
	Tokens           api.TokenUsage
	MaxContextTokens int
	LastPromptTokens int
	model            string
}

func New(model string) *Session {
	return &Session{
		Messages: []api.Message{
			{Role: api.RoleSystem, Content: SystemPromptForModel(model)},
		},
		model: model,
	}
}

func NewWithContext(model string, maxTokens int) *Session {
	return &Session{
		Messages: []api.Message{
			{Role: api.RoleSystem, Content: SystemPromptForModel(model)},
		},
		MaxContextTokens: maxTokens,
		model:            model,
	}
}

func (s *Session) AddUser(content string) {
	s.Messages = append(s.Messages, api.Message{
		Role:    api.RoleUser,
		Content: content,
	})
}

func (s *Session) AddAssistant(content string) {
	s.Messages = append(s.Messages, api.Message{
		Role:    api.RoleAssistant,
		Content: content,
	})
}

func (s *Session) Clear() {
	s.Messages = []api.Message{
		{Role: api.RoleSystem, Content: SystemPromptForModel(s.model)},
	}
	s.Tokens = api.TokenUsage{}
	s.LastPromptTokens = 0
}

func (s *Session) AddTokens(usage api.TokenUsage) {
	s.Tokens.PromptTokens += usage.PromptTokens
	s.Tokens.CompletionTokens += usage.CompletionTokens
	s.Tokens.TotalTokens += usage.TotalTokens
	if usage.PromptTokens > 0 {
		s.LastPromptTokens = usage.PromptTokens
	}
}

// EstimateTokens returns a rough estimate of the current context size
// based on character count (chars * 0.3).
func (s *Session) EstimateTokens() int {
	total := 0
	for _, msg := range s.Messages {
		total += len(msg.Content)
	}
	return int(float64(total) * 0.3)
}

// ContextPercentage returns the percentage of the context window used.
// Uses LastPromptTokens (authoritative from API) if available, otherwise
// falls back to character-based estimation.
func (s *Session) ContextPercentage() float64 {
	if s.MaxContextTokens <= 0 {
		return 0
	}
	tokens := s.LastPromptTokens
	if tokens == 0 {
		tokens = s.EstimateTokens()
	}
	pct := float64(tokens) / float64(s.MaxContextTokens) * 100
	if pct > 100 {
		pct = 100
	}
	return pct
}

// IsNearLimit returns true if context usage exceeds the given threshold (0.0 to 1.0).
func (s *Session) IsNearLimit(threshold float64) bool {
	return s.ContextPercentage() >= threshold*100
}
