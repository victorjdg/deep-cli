package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/victorjdg/deep-cli/internal/search"
)

const maxFileSize = 512 * 1024 // 512KB per file

var slashSearchMgr *search.Manager

// SetSlashSearchManager sets the search manager used by the /search command.
func SetSlashSearchManager(m *search.Manager) {
	slashSearchMgr = m
}

type slashResult struct {
	output           string
	changeModel      string // non-empty to change model
	clear            bool
	quit             bool
	listModels       bool   // triggers async model listing
	compact          bool   // triggers conversation compaction
	toggleEnhance    bool   // toggles prompt enhancement mode
	toggleAgent      bool   // toggles agent mode (tool calling)
	toggleAutoAccept bool   // toggles auto-accept mode
	initProject      bool   // triggers CONTEXT.md generation
	undo             bool   // reverts the last agent file edit
	fileContent      string // content to inject into session as user context
}

type costInfo struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	ContextPct       float64
	MaxContextTokens int
	LastPromptTokens int
	MessageCount     int
	EnhanceActive    bool
}

func handleSlashCommand(input string, currentModel string, cost costInfo) slashResult {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return slashResult{}
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/help":
		return slashResult{
			output: `Available commands:
  /help              Show this help message
  /file <path> ...   Load file(s) into conversation context
  /clear             Clear conversation history
  /compact           Summarize and compact conversation context
  /enhance           Toggle prompt enhancement (Ctrl+E)
  /agent             Toggle agent mode (tool calling)
  /auto              Toggle auto-accept (skip confirmation for file edits and commands)
  /init              Generate a CONTEXT.md file for the current project
  /undo              Revert the last file edit made by the agent
  /search [engine]   Show or change search engine (tavily, brave, searxng)
  /models            List available models from the API
  /model [name]      Show or change the current model
  /cost              Show token usage for this session
  /exit              Exit the application

Shortcuts:
  Enter          Submit message
  Ctrl+E         Toggle prompt enhancement
  Ctrl+A         Toggle auto-accept
  Ctrl+T         Toggle agent trace panel
  Ctrl+C         Cancel streaming / Quit
  Ctrl+D         Quit
  Ctrl+L         Clear screen`,
		}

	case "/file":
		if len(args) == 0 {
			return slashResult{
				output: "Usage: /file <path> [path2 ...]\nLoads file contents into the conversation context.",
			}
		}
		return loadFiles(args)

	case "/models":
		return slashResult{
			listModels: true,
		}

	case "/clear":
		return slashResult{
			output: "Conversation cleared.",
			clear:  true,
		}

	case "/model":
		if len(args) == 0 {
			return slashResult{
				output: fmt.Sprintf("Current model: %s", currentModel),
			}
		}
		return slashResult{
			output:      fmt.Sprintf("Model changed to: %s", args[0]),
			changeModel: args[0],
		}

	case "/compact":
		if cost.MessageCount <= 2 {
			return slashResult{output: "Nothing to compact. Start a conversation first."}
		}
		return slashResult{compact: true}

	case "/enhance":
		return slashResult{toggleEnhance: true}

	case "/search":
		if slashSearchMgr == nil {
			return slashResult{output: "Search not available."}
		}
		if len(args) == 0 {
			return slashResult{
				output: fmt.Sprintf("Current search engine: %s\nAvailable engines: tavily, brave, searxng\nUsage: /search <engine>", slashSearchMgr.CurrentName()),
			}
		}
		msg, err := slashSearchMgr.SetEngine(args[0])
		if err != nil {
			return slashResult{output: err.Error()}
		}
		return slashResult{output: msg}

	case "/agent":
		return slashResult{toggleAgent: true}

	case "/auto":
		return slashResult{toggleAutoAccept: true}

	case "/init":
		return slashResult{initProject: true}

	case "/undo":
		return slashResult{undo: true}

	case "/exit":
		return slashResult{quit: true}

	case "/cost":
		promptDisplay := cost.LastPromptTokens
		if promptDisplay == 0 {
			promptDisplay = cost.PromptTokens
		}
		output := fmt.Sprintf(
			"Session Token Usage:\n"+
				"  Prompt tokens:     %d\n"+
				"  Completion tokens: %d\n"+
				"  Total tokens:      %d\n"+
				"\n"+
				"Context Window:\n"+
				"  Current prompt:    %d / %d tokens\n"+
				"  Usage:             %.1f%%",
			cost.PromptTokens,
			cost.CompletionTokens,
			cost.TotalTokens,
			promptDisplay,
			cost.MaxContextTokens,
			cost.ContextPct,
		)
		return slashResult{output: output}

	default:
		return slashResult{
			output: fmt.Sprintf("Unknown command: %s. Type /help for available commands.", cmd),
		}
	}
}

// extractInlineFiles finds all /file references in a message, loads the files,
// and returns the cleaned message text plus the file content to append.
// Example: "Explain this /file main.go" → ("Explain this", fileContent, output, error)
// extractInlineFiles finds all /file references in a message, loads the files,
// and returns the cleaned message text plus the file content to append.
// Supports: "Explain /file main.go", "Review /file ./src/api.go /file /tmp/test.go"
func extractInlineFiles(input string) (message string, fileContent string, output string) {
	words := strings.Fields(input)
	var msgWords []string
	var filePaths []string

	for i := 0; i < len(words); i++ {
		if strings.ToLower(words[i]) == "/file" {
			// Collect following args that look like file paths.
			for i+1 < len(words) {
				next := words[i+1]
				// Stop if it's another slash command (but not an absolute path).
				if strings.HasPrefix(next, "/") && !looksLikePath(next) {
					break
				}
				i++
				filePaths = append(filePaths, next)
			}
		} else {
			msgWords = append(msgWords, words[i])
		}
	}

	message = strings.Join(msgWords, " ")

	if len(filePaths) == 0 {
		return message, "", ""
	}

	result := loadFiles(filePaths)
	return message, result.fileContent, result.output
}

// looksLikePath checks if a string starting with / is likely a file path
// rather than a slash command.
func looksLikePath(s string) bool {
	// Contains path separators, file extension, or known path prefixes.
	if strings.Contains(s, ".") || strings.Contains(s[1:], "/") {
		return true
	}
	// Known slash commands.
	cmds := []string{"/help", "/clear", "/compact", "/enhance", "/agent", "/auto", "/init", "/undo", "/search", "/models", "/model", "/cost", "/exit", "/file"}
	lower := strings.ToLower(s)
	for _, c := range cmds {
		if lower == c {
			return false
		}
	}
	// Default: treat as a path if it starts with / and isn't a known command.
	return true
}

func loadFiles(paths []string) slashResult {
	var contentParts []string
	var summary []string
	var errors []string

	for _, rawPath := range paths {
		// Expand ~ to home directory.
		p := rawPath
		if strings.HasPrefix(p, "~/") {
			home, err := os.UserHomeDir()
			if err == nil {
				p = filepath.Join(home, p[2:])
			}
		}

		// Resolve to absolute path.
		absPath, err := filepath.Abs(p)
		if err != nil {
			errors = append(errors, fmt.Sprintf("  %s: %s", rawPath, err))
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil {
			errors = append(errors, fmt.Sprintf("  %s: file not found", rawPath))
			continue
		}

		if info.IsDir() {
			errors = append(errors, fmt.Sprintf("  %s: is a directory", rawPath))
			continue
		}

		if info.Size() > maxFileSize {
			errors = append(errors, fmt.Sprintf("  %s: too large (%dKB, max %dKB)", rawPath, info.Size()/1024, maxFileSize/1024))
			continue
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			errors = append(errors, fmt.Sprintf("  %s: %s", rawPath, err))
			continue
		}

		// Check if file is likely binary.
		if !utf8.Valid(data) || containsNullBytes(data) {
			errors = append(errors, fmt.Sprintf("  %s: binary file (not supported)", rawPath))
			continue
		}

		ext := filepath.Ext(absPath)
		lang := extToLang(ext)
		filename := filepath.Base(absPath)

		var block string
		if lang != "" {
			block = fmt.Sprintf("File: %s\n```%s\n%s\n```", filename, lang, string(data))
		} else {
			block = fmt.Sprintf("File: %s\n```\n%s\n```", filename, string(data))
		}

		contentParts = append(contentParts, block)
		lines := strings.Count(string(data), "\n") + 1
		summary = append(summary, fmt.Sprintf("  + %s (%d lines)", filename, lines))
	}

	var output strings.Builder

	if len(summary) > 0 {
		output.WriteString("Files loaded into context:\n")
		output.WriteString(strings.Join(summary, "\n"))
	}

	if len(errors) > 0 {
		if output.Len() > 0 {
			output.WriteString("\n")
		}
		output.WriteString("Errors:\n")
		output.WriteString(strings.Join(errors, "\n"))
	}

	if len(contentParts) == 0 {
		return slashResult{output: output.String()}
	}

	fileContent := "I'm sharing the following file(s) for context:\n\n" + strings.Join(contentParts, "\n\n")

	return slashResult{
		output:      output.String(),
		fileContent: fileContent,
	}
}

func containsNullBytes(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func extToLang(ext string) string {
	m := map[string]string{
		".go":    "go",
		".py":    "python",
		".js":    "javascript",
		".ts":    "typescript",
		".tsx":   "tsx",
		".jsx":   "jsx",
		".rs":    "rust",
		".rb":    "ruby",
		".java":  "java",
		".c":     "c",
		".cpp":   "cpp",
		".h":     "c",
		".hpp":   "cpp",
		".cs":    "csharp",
		".php":   "php",
		".swift": "swift",
		".kt":    "kotlin",
		".scala": "scala",
		".sh":    "bash",
		".bash":  "bash",
		".zsh":   "zsh",
		".fish":  "fish",
		".sql":   "sql",
		".html":  "html",
		".css":   "css",
		".scss":  "scss",
		".json":  "json",
		".yaml":  "yaml",
		".yml":   "yaml",
		".toml":  "toml",
		".xml":   "xml",
		".md":    "markdown",
		".lua":   "lua",
		".r":     "r",
		".dart":  "dart",
		".zig":   "zig",
		".ex":    "elixir",
		".exs":   "elixir",
		".erl":   "erlang",
		".hs":    "haskell",
		".ml":    "ocaml",
		".vim":   "vim",
		".tf":    "hcl",
		".proto": "protobuf",
		".dockerfile": "dockerfile",
		".makefile":   "makefile",
	}
	return m[strings.ToLower(ext)]
}
