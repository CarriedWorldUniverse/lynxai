package bridlecfg

import (
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	bridle "github.com/CarriedWorldUniverse/bridle"
	openaiProv "github.com/CarriedWorldUniverse/bridle/provider/openai"
)

// NewHarness constructs a *bridle.Harness for cfg. Only openai-api in v1.
func NewHarness(cfg *Config) (*bridle.Harness, error) {
	switch cfg.Provider {
	case "openai-api":
		client := openai.NewClient(
			option.WithAPIKey(cfg.APIKey),
			option.WithBaseURL(cfg.BaseURL),
		)
		return bridle.NewHarness(openaiProv.NewWithClient(&client)), nil
	default:
		return nil, fmt.Errorf("bridlecfg: provider %q not supported in v1 (use openai-api)", cfg.Provider)
	}
}
