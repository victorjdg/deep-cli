package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/victorjdg/deep-cli/internal/api"
)

// saveConversation writes messages to a markdown file and returns the path used.
// If outPath is empty, a timestamped file is created in the current directory.
func saveConversation(messages []api.Message, outPath string) (string, error) {
	// Filter out system messages for the export.
	var visible []api.Message
	for _, m := range messages {
		if m.Role != api.RoleSystem {
			visible = append(visible, m)
		}
	}
	if len(visible) == 0 {
		return "", fmt.Errorf("no messages to save")
	}

	if outPath == "" {
		ts := time.Now().Format("2006-01-02_15-04-05")
		outPath = fmt.Sprintf("conversation_%s.md", ts)
	}

	// Resolve relative to cwd.
	if !filepath.IsAbs(outPath) {
		cwd, err := os.Getwd()
		if err == nil {
			outPath = filepath.Join(cwd, outPath)
		}
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", err
	}

	var sb strings.Builder
	ts := time.Now().Format("2006-01-02 15:04:05")
	sb.WriteString(fmt.Sprintf("# Conversation — %s\n\n", ts))

	for _, msg := range visible {
		switch msg.Role {
		case api.RoleUser:
			sb.WriteString("## User\n\n")
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		case api.RoleAssistant:
			sb.WriteString("## Assistant\n\n")
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		}
	}

	if err := os.WriteFile(outPath, []byte(strings.TrimRight(sb.String(), "\n")+"\n"), 0o644); err != nil {
		return "", err
	}
	return outPath, nil
}
