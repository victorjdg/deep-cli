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
	APIURL           string
	MaxContextTokens int
	MaxSubagents     int
}

var defaultContextSizes = map[string]int{
	"deepseek-v4-pro":   1000000,
	"deepseek-v4-flash": 1000000,
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

	if apiKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY is required. Set it via the environment variable or --api-key flag")
	}

	model := v.GetString("model")
	if model == "" {
		model = os.Getenv("DEEPSEEK_MODEL")
	}
	if model == "" {
		model = "deepseek-v4-pro"
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

	maxSubagents := v.GetInt("max-subagents")
	if maxSubagents == 0 {
		if envMax := os.Getenv("DEEPSEEK_MAX_SUBAGENTS"); envMax != "" {
			if n, err := strconv.Atoi(envMax); err == nil && n > 0 {
				maxSubagents = n
			}
		}
	}
	if maxSubagents == 0 {
		maxSubagents = 5
	}

	return &Config{
		APIKey:           apiKey,
		Model:            model,
		APIURL:           "https://api.deepseek.com/chat/completions",
		MaxContextTokens: maxContext,
		MaxSubagents:     maxSubagents,
	}, nil
}
