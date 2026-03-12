package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/victorjdg/deep-cli/internal/api"
	"github.com/victorjdg/deep-cli/internal/search"
)

const (
	maxEntries       = 200
	maxSearchMatches = 100
	maxGlobResults   = 100
)

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
		{
			Type: "function",
			Function: api.FunctionSchema{
				Name:        "search_files",
				Description: "Search for a text pattern (regex) across files in a directory. Returns matching lines with file path and line number.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Regular expression or literal text to search for.",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Directory to search in. Defaults to '.' (current directory).",
						},
						"case_insensitive": map[string]interface{}{
							"type":        "boolean",
							"description": "If true, search is case-insensitive. Defaults to false.",
						},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			Type: "function",
			Function: api.FunctionSchema{
				Name:        "patch_file",
				Description: "Apply a surgical edit to a file by replacing an exact string. Fails if the string is not found or appears more than once. Prefer this over write_file for small changes.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the file to edit.",
						},
						"old_string": map[string]interface{}{
							"type":        "string",
							"description": "The exact string to find and replace. Must appear exactly once in the file.",
						},
						"new_string": map[string]interface{}{
							"type":        "string",
							"description": "The replacement string.",
						},
					},
					"required": []string{"path", "old_string", "new_string"},
				},
			},
		},
		{
			Type: "function",
			Function: api.FunctionSchema{
				Name:        "glob",
				Description: "Find files matching a glob pattern (e.g. '**/*.go', '*.md'). Returns matching file paths relative to the base directory.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"pattern": map[string]interface{}{
							"type":        "string",
							"description": "Glob pattern to match filenames (e.g. '*.go', '*.ts').",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Base directory to search in. Defaults to '.' (current directory).",
						},
					},
					"required": []string{"pattern"},
				},
			},
		},
		{
			Type: "function",
			Function: api.FunctionSchema{
				Name:        "read_multiple_files",
				Description: "Read the contents of multiple files in a single call. More efficient than calling read_file repeatedly.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"paths": map[string]interface{}{
							"type":        "array",
							"description": "List of file paths to read.",
							"items": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"required": []string{"paths"},
				},
			},
		},
		{
			Type: "function",
			Function: api.FunctionSchema{
				Name:        "get_file_info",
				Description: "Get metadata about a file or directory: size, modification time, permissions, type, and MIME type.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the file or directory.",
						},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: api.FunctionSchema{
				Name:        "run_command",
				Description: "Run a shell command and return its output. The command will be shown to the user for approval before execution. Use for build, test, lint, or other development tasks.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"description": "Shell command to run (executed via sh -c).",
						},
					},
					"required": []string{"command"},
				},
			},
		},
		{
			Type: "function",
			Function: api.FunctionSchema{
				Name:        "fetch_url",
				Description: "Fetch the text content of a web page. Use this after web_search to read the full content of a relevant URL. Returns clean plain text extracted from the page.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{
							"type":        "string",
							"description": "The URL to fetch.",
						},
					},
					"required": []string{"url"},
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
	case "search_files":
		return execSearchFiles(argsJSON, workDir)
	case "patch_file":
		return execPatchFile(argsJSON, workDir)
	case "glob":
		return execGlob(argsJSON, workDir)
	case "read_multiple_files":
		return execReadMultipleFiles(argsJSON, workDir)
	case "get_file_info":
		return execGetFileInfo(argsJSON, workDir)
	case "fetch_url":
		return execFetchURL(argsJSON)
	case "run_command":
		return "", fmt.Errorf("run_command must be handled by the agent loop, not tools.Execute")
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

type searchFilesArgs struct {
	Query           string `json:"query"`
	Path            string `json:"path"`
	CaseInsensitive bool   `json:"case_insensitive"`
}

func execSearchFiles(argsJSON string, workDir string) (string, error) {
	var args searchFilesArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Query == "" {
		return "", fmt.Errorf("query must not be empty")
	}

	searchPath := args.Path
	if searchPath == "" {
		searchPath = "."
	}
	base, err := safePath(searchPath, workDir)
	if err != nil {
		return "", err
	}

	pattern := args.Query
	if args.CaseInsensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	var matches []string
	err = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if len(matches) >= maxSearchMatches {
			return filepath.SkipAll
		}

		// Skip binary files.
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		header := make([]byte, 512)
		n, _ := f.Read(header)
		f.Close()
		for _, b := range header[:n] {
			if b == 0 {
				return nil // binary file
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(workDir, path)
		lines := strings.Split(string(data), "\n")
		for lineNum, line := range lines {
			if len(matches) >= maxSearchMatches {
				break
			}
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d: %s", rel, lineNum+1, line))
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search error: %w", err)
	}

	if len(matches) == 0 {
		return "(no matches)", nil
	}
	result := strings.Join(matches, "\n")
	if len(matches) == maxSearchMatches {
		result += fmt.Sprintf("\n... (results capped at %d)", maxSearchMatches)
	}
	return result, nil
}

type patchFileArgs struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func execPatchFile(argsJSON string, workDir string) (string, error) {
	var args patchFileArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if args.OldString == "" {
		return "", fmt.Errorf("old_string must not be empty")
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
		return "", fmt.Errorf("%s is a directory", args.Path)
	}
	if info.Size() > 512*1024 {
		return "", fmt.Errorf("file too large (%dKB, max 512KB)", info.Size()/1024)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("cannot read file: %w", err)
	}
	content := string(data)

	count := strings.Count(content, args.OldString)
	if count == 0 {
		return "", fmt.Errorf("old_string not found in %s", args.Path)
	}
	if count > 1 {
		return "", fmt.Errorf("old_string appears %d times in %s; it must be unique for a safe patch", count, args.Path)
	}

	patched := strings.Replace(content, args.OldString, args.NewString, 1)
	if err := os.WriteFile(target, []byte(patched), info.Mode()); err != nil {
		return "", fmt.Errorf("cannot write file: %w", err)
	}

	rel, _ := filepath.Rel(workDir, target)
	return fmt.Sprintf("patched %s (%d bytes written)", rel, len(patched)), nil
}

type globArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func execGlob(argsJSON string, workDir string) (string, error) {
	var args globArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Pattern == "" {
		return "", fmt.Errorf("pattern must not be empty")
	}

	basePath := args.Path
	if basePath == "" {
		basePath = "."
	}
	base, err := safePath(basePath, workDir)
	if err != nil {
		return "", err
	}

	var results []string
	err = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if len(results) >= maxGlobResults {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(base, path)
		// Match against the filename for simple patterns, or the full relative path for path-containing patterns.
		matchTarget := info.Name()
		if strings.ContainsRune(args.Pattern, '/') {
			matchTarget = rel
		}
		matched, err := filepath.Match(args.Pattern, matchTarget)
		if err != nil {
			return fmt.Errorf("invalid pattern: %w", err)
		}
		if matched {
			relFromWork, _ := filepath.Rel(workDir, path)
			results = append(results, relFromWork)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("glob error: %w", err)
	}

	if len(results) == 0 {
		return "(no matches)", nil
	}
	result := strings.Join(results, "\n")
	if len(results) == maxGlobResults {
		result += fmt.Sprintf("\n... (results capped at %d)", maxGlobResults)
	}
	return result, nil
}

type readMultipleFilesArgs struct {
	Paths []string `json:"paths"`
}

func execReadMultipleFiles(argsJSON string, workDir string) (string, error) {
	var args readMultipleFilesArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if len(args.Paths) == 0 {
		return "", fmt.Errorf("paths must not be empty")
	}

	var sb strings.Builder
	for _, p := range args.Paths {
		target, err := safePath(p, workDir)
		if err != nil {
			sb.WriteString(fmt.Sprintf("--- %s: ERROR: %s ---\n\n", p, err))
			continue
		}
		info, err := os.Stat(target)
		if err != nil {
			sb.WriteString(fmt.Sprintf("--- %s: ERROR: file not found ---\n\n", p))
			continue
		}
		if info.IsDir() {
			sb.WriteString(fmt.Sprintf("--- %s: ERROR: is a directory ---\n\n", p))
			continue
		}
		if info.Size() > 512*1024 {
			sb.WriteString(fmt.Sprintf("--- %s: ERROR: file too large (%dKB, max 512KB) ---\n\n", p, info.Size()/1024))
			continue
		}
		data, err := os.ReadFile(target)
		if err != nil {
			sb.WriteString(fmt.Sprintf("--- %s: ERROR: %s ---\n\n", p, err))
			continue
		}
		sb.WriteString(fmt.Sprintf("--- %s ---\n%s\n\n", p, string(data)))
	}

	return strings.TrimRight(sb.String(), "\n"), nil
}

type getFileInfoArgs struct {
	Path string `json:"path"`
}

func execGetFileInfo(argsJSON string, workDir string) (string, error) {
	var args getFileInfoArgs
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

	fileType := "file"
	mimeType := ""
	if info.IsDir() {
		fileType = "directory"
	} else {
		ext := filepath.Ext(info.Name())
		mimeType = mime.TypeByExtension(ext)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("path:     %s\n", args.Path))
	sb.WriteString(fmt.Sprintf("type:     %s\n", fileType))
	sb.WriteString(fmt.Sprintf("size:     %d bytes\n", info.Size()))
	sb.WriteString(fmt.Sprintf("modified: %s\n", info.ModTime().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("mode:     %s\n", info.Mode().String()))
	if mimeType != "" {
		sb.WriteString(fmt.Sprintf("mime:     %s\n", mimeType))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

const (
	maxFetchBytes   = 512 * 1024 // 512 KB raw HTML limit
	maxFetchContent = 8000       // chars of clean text returned to the model
	fetchTimeout    = 15 * time.Second
)

type fetchURLArgs struct {
	URL string `json:"url"`
}

func execFetchURL(argsJSON string) (string, error) {
	var args fetchURLArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if args.URL == "" {
		return "", fmt.Errorf("url must not be empty")
	}

	// Use Tavily Extract API if Tavily is the active search engine.
	if searchMgr != nil && searchMgr.CurrentName() == "tavily" {
		if result, err := tavilyExtract(args.URL); err == nil {
			return result, nil
		}
		// Fall through to generic fetch on Tavily error.
	}

	return genericFetch(args.URL)
}

// tavilyExtract uses the Tavily /extract endpoint for high-quality content extraction.
func tavilyExtract(targetURL string) (string, error) {
	apiKey := searchMgr.Current().(*search.Tavily).APIKey()
	if apiKey == "" {
		return "", fmt.Errorf("no Tavily API key")
	}

	body, _ := json.Marshal(map[string]interface{}{
		"api_key": apiKey,
		"urls":    []string{targetURL},
	})

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/extract", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("tavily extract failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tavily extract returned status %d", resp.StatusCode)
	}

	var result struct {
		Results []struct {
			URL        string `json:"url"`
			RawContent string `json:"raw_content"`
		} `json:"results"`
		FailedResults []struct {
			URL   string `json:"url"`
			Error string `json:"error"`
		} `json:"failed_results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Results) == 0 {
		if len(result.FailedResults) > 0 {
			return "", fmt.Errorf("tavily extract failed for URL: %s", result.FailedResults[0].Error)
		}
		return "", fmt.Errorf("tavily extract returned no content")
	}

	content := strings.TrimSpace(result.Results[0].RawContent)
	return truncateContent(content), nil
}

// genericFetch downloads a URL and extracts plain text from the HTML.
func genericFetch(targetURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	// Mimic a real browser to avoid bot-blocking.
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, maxFetchBytes)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	text := extractText(string(raw))
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("page content is not valid UTF-8")
	}
	if text == "" {
		return "(no readable text content found)", nil
	}
	return truncateContent(text), nil
}

// tagPattern matches HTML tags, scripts, styles and comments.
var (
	reScript  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	reNav     = regexp.MustCompile(`(?is)<(nav|header|footer|aside|menu)[^>]*>.*?</(nav|header|footer|aside|menu)>`)
	reTag     = regexp.MustCompile(`<[^>]+>`)
	reSpaces  = regexp.MustCompile(`[ \t]+`)
	reLines   = regexp.MustCompile(`\n{3,}`)
)

// extractText removes HTML markup and returns clean plain text.
func extractText(html string) string {
	html = reScript.ReplaceAllString(html, " ")
	html = reStyle.ReplaceAllString(html, " ")
	html = reComment.ReplaceAllString(html, " ")
	html = reNav.ReplaceAllString(html, " ")
	// Replace block-level tags with newlines to preserve paragraph structure.
	for _, tag := range []string{"p", "div", "li", "h1", "h2", "h3", "h4", "h5", "h6", "br", "tr"} {
		html = strings.ReplaceAll(html, "<"+tag, "\n<"+tag)
		html = strings.ReplaceAll(html, "</"+tag+">", "\n")
	}
	text := reTag.ReplaceAllString(html, "")
	// Decode common HTML entities.
	text = strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&nbsp;", " ",
		"&mdash;", "—", "&ndash;", "–", "&hellip;", "...",
	).Replace(text)
	text = reSpaces.ReplaceAllString(text, " ")
	text = reLines.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// truncateContent caps the content at maxFetchContent chars with a notice.
func truncateContent(s string) string {
	if len(s) <= maxFetchContent {
		return s
	}
	return s[:maxFetchContent] + "\n\n... (content truncated)"
}

// ReadPrevious reads the current content of a file before it is overwritten,
// returning (content, true) if it exists or ("", false) if it is a new file.
// Used by the agent loop to build undo entries before write_file / patch_file.
func ReadPrevious(path, workDir string) (string, bool) {
	target, err := safePath(path, workDir)
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", false // file doesn't exist yet
	}
	return string(data), true
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
