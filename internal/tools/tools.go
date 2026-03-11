package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/victorjdg/deep-cli/internal/api"
	"github.com/victorjdg/deep-cli/internal/search"
)

const maxEntries = 200

var searchMgr *search.Manager

// SetSearchManager sets the search manager used by the web_search tool.
func SetSearchManager(m *search.Manager) {
	searchMgr = m
}

// Definitions returns the tool definitions to send with API requests.
func Definitions() []api.ToolDefinition {
	return []api.ToolDefinition{
		{
			Type: "function",
			Function: api.FunctionSchema{
				Name:        "list_files",
				Description: "List files and directories at a given path. Returns names with '/' suffix for directories.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Directory path to list. Use '.' for current directory.",
						},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: api.FunctionSchema{
				Name:        "read_file",
				Description: "Read the contents of a file. Returns the file content as text.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the file to read.",
						},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: api.FunctionSchema{
				Name:        "write_file",
				Description: "Write text content to a file. Creates the file if it does not exist, overwrites it if it does. Parent directories are created automatically. Constrained to the working directory.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the file to write, relative to the working directory.",
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "The text content to write to the file.",
						},
					},
					"required": []string{"path", "content"},
				},
			},
		},
		{
			Type: "function",
			Function: api.FunctionSchema{
				Name:        "web_search",
				Description: "Search the web for current information. Use this when you need up-to-date facts, documentation, or answers not in your training data.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "The search query.",
						},
					},
					"required": []string{"query"},
				},
			},
		},
	}
}

// Execute runs a tool by name with the given JSON arguments.
// workDir is used as the security boundary — paths cannot escape it.
func Execute(name string, argsJSON string, workDir string) (string, error) {
	switch name {
	case "list_files":
		return execListFiles(argsJSON, workDir)
	case "read_file":
		return execReadFile(argsJSON, workDir)
	case "write_file":
		return execWriteFile(argsJSON, workDir)
	case "web_search":
		return execWebSearch(argsJSON)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

type listFilesArgs struct {
	Path string `json:"path"`
}

func execListFiles(argsJSON string, workDir string) (string, error) {
	var args listFilesArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	target, err := safePath(args.Path, workDir)
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return "", fmt.Errorf("cannot read directory: %w", err)
	}

	var lines []string
	for i, entry := range entries {
		if i >= maxEntries {
			lines = append(lines, fmt.Sprintf("... and %d more entries", len(entries)-maxEntries))
			break
		}
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		lines = append(lines, name)
	}

	if len(lines) == 0 {
		return "(empty directory)", nil
	}
	return strings.Join(lines, "\n"), nil
}

type readFileArgs struct {
	Path string `json:"path"`
}

func execReadFile(argsJSON string, workDir string) (string, error) {
	var args readFileArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	target, err := safePath(args.Path, workDir)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("file not found: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, use list_files instead", args.Path)
	}
	if info.Size() > 512*1024 {
		return "", fmt.Errorf("file too large (%dKB, max 512KB)", info.Size()/1024)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("cannot read file: %w", err)
	}
	return string(data), nil
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

const maxWriteSize = 4 * 1024 * 1024 // 4 MB

func execWriteFile(argsJSON string, workDir string) (string, error) {
	var args writeFileArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if args.Path == "" {
		return "", fmt.Errorf("path must not be empty")
	}

	if strings.HasSuffix(args.Path, "/") || strings.HasSuffix(args.Path, string(filepath.Separator)) {
		return "", fmt.Errorf("path must be a file, not a directory")
	}

	target, err := safePath(args.Path, workDir)
	if err != nil {
		return "", err
	}

	if info, statErr := os.Stat(target); statErr == nil && info.IsDir() {
		return "", fmt.Errorf("%s is an existing directory, cannot overwrite with a file", args.Path)
	}

	if len(args.Content) > maxWriteSize {
		return "", fmt.Errorf("content too large (%d bytes, max %d bytes)", len(args.Content), maxWriteSize)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return "", fmt.Errorf("cannot create parent directories: %w", err)
	}

	if err := os.WriteFile(target, []byte(args.Content), 0644); err != nil {
		return "", fmt.Errorf("cannot write file: %w", err)
	}

	rel, _ := filepath.Rel(workDir, target)
	return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), rel), nil
}

type webSearchArgs struct {
	Query string `json:"query"`
}

func execWebSearch(argsJSON string) (string, error) {
	if searchMgr == nil {
		return "", fmt.Errorf("search not configured")
	}
	var args webSearchArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	results, err := searchMgr.Current().Search(context.Background(), args.Query, 5)
	if err != nil {
		return "", err
	}
	return search.FormatResults(args.Query, results), nil
}

// safePath resolves a path relative to workDir and ensures it doesn't escape it.
func safePath(path string, workDir string) (string, error) {
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(workDir, path))
	}

	// Ensure the resolved path is within workDir.
	rel, err := filepath.Rel(workDir, abs)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("access denied: path %s is outside working directory", path)
	}

	return abs, nil
}
