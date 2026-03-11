package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	APIKey           string
	Model            string
	UseLocal         bool
	OllamaHost       string
	APIURL           string
	MaxContextTokens int
}

var defaultContextSizes = map[string]int{
	"deepseek-chat":     128000,
	"deepseek-reasoner": 128000,
}

const fallbackContextSize = 8192

// lookupContextSize finds the best matching context size for a model name
// using prefix matching: "deepseek-coder:6.7b-instruct-q4_0" matches
// "deepseek-coder:6.7b" first, then "deepseek-coder", then fallback.
func lookupContextSize(model string) int {
	// Try exact match first.
	if size, ok := defaultContextSizes[model]; ok {
		return size
	}

	// Try progressively shorter prefixes.
	best := ""
	for key := range defaultContextSizes {
		if strings.HasPrefix(model, key) && len(key) > len(best) {
			best = key
		}
	}
	if best != "" {
		return defaultContextSizes[best]
	}

	return fallbackContextSize
}

func Load() (*Config, error) {
	v := viper.GetViper()

	apiKey := v.GetString("api-key")
	if apiKey == "" {
		apiKey = os.Getenv("DEEPSEEK_API_KEY")
	}

	model := v.GetString("model")
	if model == "" {
		model = os.Getenv("DEEPSEEK_MODEL")
	}
	// Model default is set after useLocal is resolved (see below).

	ollamaHost := v.GetString("ollama-host")
	if ollamaHost == "" {
		ollamaHost = os.Getenv("OLLAMA_HOST")
	}
	if ollamaHost == "" {
		ollamaHost = "http://localhost:11434"
	}

	useLocal := v.GetBool("local")
	if !useLocal {
		envLocal := os.Getenv("DEEPSEEK_USE_LOCAL")
		if envLocal == "true" {
			useLocal = true
		} else if apiKey == "" {
			useLocal = true
		}
	}

	if !useLocal && apiKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY is required for cloud mode. Set it or use --local flag")
	}

	// Set default model based on mode.
	if model == "" {
		if useLocal {
			model = "deepseek-coder:6.7b"
		} else {
			model = "deepseek-chat"
		}
	}

	// Resolve max context tokens: flag > env > model lookup > fallback.
	maxContext := v.GetInt("max-context")
	if maxContext == 0 {
		if envMax := os.Getenv("DEEPSEEK_MAX_CONTEXT"); envMax != "" {
			if n, err := strconv.Atoi(envMax); err == nil && n > 0 {
				maxContext = n
			}
		}
	}
	if maxContext == 0 {
		maxContext = lookupContextSize(model)
	}

	return &Config{
		APIKey:           apiKey,
		Model:            model,
		UseLocal:         useLocal,
		OllamaHost:       ollamaHost,
		APIURL:           "https://api.deepseek.com/chat/completions",
		MaxContextTokens: maxContext,
	}, nil
}
