package bridlecfg

import (
	"testing"
)

func TestSynthesizeDefault_DeepSeek(t *testing.T) {
	t.Setenv("LYNXAI_LLM_API_KEY", "sk-test")
	cfg, err := Synthesize("") // empty path => synthesize default
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://api.deepseek.com" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Model != "deepseek-chat" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if cfg.APIKey != "sk-test" {
		t.Errorf("APIKey not picked up from env")
	}
}

func TestSynthesizeDefault_MissingKeyIsError(t *testing.T) {
	t.Setenv("LYNXAI_LLM_API_KEY", "")
	_, err := Synthesize("")
	if err == nil {
		t.Fatal("expected error when no key provided")
	}
}
