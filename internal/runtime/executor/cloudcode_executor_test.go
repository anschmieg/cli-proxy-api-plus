package executor

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestCloudCodeExecutor_Routing(t *testing.T) {
	cfg := &config.Config{}
	e := NewCloudCodeExecutor(cfg)

	tests := []struct {
		model    string
		expected string
	}{
		{"claude-sonnet-4-5", "antigravity"},
		{"gemini-3-pro", "antigravity"},
		{"antigravity-claude", "antigravity"},
		{"gemini-2.5-pro", "gemini-cli"},
		{"gemini-1.5-flash", "gemini-cli"},
		{"unknown-model", "gemini-cli"},
	}

	for _, tt := range tests {
		target := e.getTargetExecutor(tt.model)
		if target.Identifier() != tt.expected {
			t.Errorf("model %s: expected executor %s, got %s", tt.model, tt.expected, target.Identifier())
		}
	}
}
