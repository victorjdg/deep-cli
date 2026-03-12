package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// keyFilePatterns are files read in full when found at the project root.
var keyFilePatterns = []string{
	"README.md", "README.txt", "README",
	"go.mod", "go.sum",
	"package.json",
	"Cargo.toml",
	"pyproject.toml", "setup.py", "requirements.txt",
	"Makefile", "makefile",
	"Dockerfile", "docker-compose.yml", "docker-compose.yaml",
	".env.example",
	"CLAUDE.md", "CONTEXT.md",
}

// ignoreDirs are directories skipped during tree traversal.
var ignoreDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, ".cache": true,
	"__pycache__": true, ".venv": true, "venv": true,
	"target": true, ".idea": true, ".vscode": true,
}

const (
	maxTreeDepth    = 4
	maxKeyFileSize  = 32 * 1024 // 32 KB per key file
	maxTotalKeySize = 128 * 1024 // 128 KB total key files
)

// buildProjectTree returns an indented directory tree string.
func buildProjectTree(root string) string {
	var sb strings.Builder
	sb.WriteString(filepath.Base(root) + "/\n")
	walkTree(&sb, root, "", 0)
	return sb.String()
}

func walkTree(sb *strings.Builder, dir, prefix string, depth int) {
	if depth >= maxTreeDepth {
		sb.WriteString(prefix + "└── ...\n")
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Filter ignored dirs and hidden files at depth 0.
	var visible []os.DirEntry
	for _, e := range entries {
		name := e.Name()
		if ignoreDirs[name] {
			continue
		}
		if depth > 0 && strings.HasPrefix(name, ".") {
			continue
		}
		visible = append(visible, e)
	}

	for i, entry := range visible {
		isLast := i == len(visible)-1
		connector := "├── "
		childPrefix := prefix + "│   "
		if isLast {
			connector = "└── "
			childPrefix = prefix + "    "
		}
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		sb.WriteString(prefix + connector + name + "\n")
		if entry.IsDir() {
			walkTree(sb, filepath.Join(dir, entry.Name()), childPrefix, depth+1)
		}
	}
}

// collectKeyFiles reads key files from the project root and returns their content.
func collectKeyFiles(root string) string {
	var parts []string
	totalSize := 0

	for _, pattern := range keyFilePatterns {
		path := filepath.Join(root, pattern)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > maxKeyFileSize {
			parts = append(parts, fmt.Sprintf("### %s\n*(file too large to include, %d KB)*", pattern, info.Size()/1024))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if !utf8.Valid(data) || containsNullBytes(data) {
			continue
		}
		totalSize += len(data)
		if totalSize > maxTotalKeySize {
			parts = append(parts, fmt.Sprintf("### %s\n*(skipped: total key file budget exceeded)*", pattern))
			continue
		}
		ext := filepath.Ext(pattern)
		lang := extToLang(ext)
		if lang == "" {
			lang = strings.TrimPrefix(ext, ".")
		}
		parts = append(parts, fmt.Sprintf("### %s\n```%s\n%s\n```", pattern, lang, string(data)))
	}

	if len(parts) == 0 {
		return "(no key files found)"
	}
	return strings.Join(parts, "\n\n")
}

const initSummaryDelimiter = "---SUMMARY---"

// buildInitPrompt assembles the full prompt sent to the model.
func buildInitPrompt(root string) string {
	tree := buildProjectTree(root)
	keyFiles := collectKeyFiles(root)

	return fmt.Sprintf(`You are analyzing a software project to generate a CONTEXT.md file.

## Project structure

`+"```"+`
%s
`+"```"+`

## Key files

%s

## Instructions

Generate a CONTEXT.md file for this project. The file must:
- Start with a one-paragraph summary of what the project does and its purpose
- List the tech stack and key dependencies
- Describe the high-level architecture and how the main components relate to each other
- Explain the key directories and what lives in each one
- Include the commands needed to build, run, and test the project
- Note any important conventions, patterns, or decisions found in the code

Be concise and factual. Do not invent information not present in the files above.

After the full CONTEXT.md content, append exactly this delimiter on its own line:
%s
Then write a 3-5 bullet point summary (plain text, no markdown headers) of the most important things you found about the project. This summary will be shown to the user in the terminal.`,
		tree, keyFiles, initSummaryDelimiter)
}

// splitInitResponse separates the CONTEXT.md content from the terminal summary.
// Returns (contextContent, summary).
func splitInitResponse(response string) (string, string) {
	idx := strings.Index(response, initSummaryDelimiter)
	if idx == -1 {
		return strings.TrimSpace(response), ""
	}
	content := strings.TrimSpace(response[:idx])
	summary := strings.TrimSpace(response[idx+len(initSummaryDelimiter):])
	return content, summary
}
