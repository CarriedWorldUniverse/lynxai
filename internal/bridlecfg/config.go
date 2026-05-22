// Package bridlecfg loads (or synthesizes) the bridle configuration used by
// lynxai's extract pipeline. v1 supports two modes:
//   - operator-supplied config file path (full bridle config, all options)
//   - zero-config default: DeepSeek via openai-api, API key from env
package bridlecfg

import (
	"fmt"
	"os"
)

// Config is the subset of bridle config lynxai builds the harness from.
type Config struct {
	Provider string // "openai-api" | "claude-api" | "ollama-local" | ...
	BaseURL  string // for openai-api: e.g. https://api.deepseek.com
	Model    string
	APIKey   string
}

// Synthesize returns a Config. If path is non-empty, it's parsed (real bridle
// config file). If path is empty, the default DeepSeek config is synthesized
// from the LYNXAI_LLM_API_KEY env var.
func Synthesize(path string) (*Config, error) {
	if path != "" {
		return loadFile(path)
	}
	key := os.Getenv("LYNXAI_LLM_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("no bridle config and LYNXAI_LLM_API_KEY not set (need either)")
	}
	return &Config{
		Provider: "openai-api",
		BaseURL:  "https://api.deepseek.com",
		Model:    "deepseek-chat",
		APIKey:   key,
	}, nil
}

// loadFile parses a bridle config file. File-based config loading is deferred
// to a later spec. v1 only supports the default-from-env path (no flag).
//
// If you reach this function, you passed --bridle-config or LYNXAI_BRIDLE_CONFIG.
// In v1, omit the flag and set LYNXAI_LLM_API_KEY instead.
func loadFile(path string) (*Config, error) {
	return nil, fmt.Errorf(
		"--bridle-config / LYNXAI_BRIDLE_CONFIG is not supported in v1 "+
			"(got path %q). Omit the flag and set LYNXAI_LLM_API_KEY for the default DeepSeek config; "+
			"file-based bridle config is planned for a later release.",
		path)
}
