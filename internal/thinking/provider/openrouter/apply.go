package openrouter

import (
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Applier implements thinking.ProviderApplier for OpenRouter models.
//
// OpenRouter often passes parameters through to the underlying model provider.
// For now, we will follow OpenAI-compatible behavior for thinking parameters
// (e.g. reasoning_effort) if the underlying model supports it.
type Applier struct{}

var _ thinking.ProviderApplier = (*Applier)(nil)

// NewApplier creates a new OpenRouter thinking applier.
func NewApplier() *Applier {
	return &Applier{}
}

func init() {
	thinking.RegisterProvider("openrouter", NewApplier())
}

// Apply applies thinking configuration to OpenRouter request body.
// Currently reuses the logic compatible with OpenAI as a safe default.
func (a *Applier) Apply(body []byte, config thinking.ThinkingConfig, modelInfo *registry.ModelInfo) ([]byte, error) {
	// If no specific model info or thinking config, default to generic handling
	if len(body) == 0 || !gjson.ValidBytes(body) {
		body = []byte(`{}`)
	}

	// Example: map reasoning_effort if present in config
	if config.Mode == thinking.ModeLevel && config.Level != "" {
		result, _ := sjson.SetBytes(body, "reasoning_effort", string(config.Level))
		return result, nil
	}

	return body, nil
}
