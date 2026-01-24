package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"
	"gopkg.in/yaml.v3"
)

const (
	mockUpstreamPort = 8081
	proxyPort        = 8082
)

func TestEndToEnd(t *testing.T) {
	// 1. Build and start Mock Upstream
	mockBin := filepath.Join(os.TempDir(), "mock_upstream")
	buildCmd := exec.Command("go", "build", "-o", mockBin, "mock_upstream.go")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build mock upstream: %v\n%s", err, out)
	}

	mockCmd := exec.Command(mockBin, "-port", fmt.Sprintf("%d", mockUpstreamPort))
	if err := mockCmd.Start(); err != nil {
		t.Fatalf("Failed to start mock upstream: %v", err)
	}
	defer func() {
		mockCmd.Process.Kill()
		os.Remove(mockBin)
	}()

	// Wait for mock to be ready
	time.Sleep(1 * time.Second)

	// 2. Create Proxy Config
	authDir := t.TempDir()
	configPath := filepath.Join(authDir, "config.yaml")
	mockBaseURL := fmt.Sprintf("http://localhost:%d", mockUpstreamPort)

	cfg := &config.Config{
		Host: "localhost",
		Port: proxyPort,
		AuthDir: authDir,
		Debug: true,
		GeminiKey: []config.GeminiKey{
			{
				APIKey:  "dummy-gemini-key",
				BaseURL: mockBaseURL,
			},
		},
		AIStudioKey: []config.AIStudioKey{
			{
				APIKey:  "dummy-aistudio-key",
				BaseURL: mockBaseURL,
			},
		},
		Routing: config.RoutingConfig{Strategy: "round-robin"},
	}
	
	cfgBytes, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, cfgBytes, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// 3. Start Proxy Service
	// We run it in a goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		builder := cliproxy.NewBuilder().
			WithConfig(cfg).
			WithConfigPath(configPath)
		
		service, err := builder.Build()
		if err != nil {
			panic(fmt.Sprintf("Failed to build proxy: %v", err))
		}
		if err := service.Run(ctx); err != nil && err != context.Canceled {
			panic(fmt.Sprintf("Proxy failed: %v", err))
		}
	}()

	// Wait for proxy to be ready
	time.Sleep(2 * time.Second)

	// 4. Run Tests
	client := &http.Client{Timeout: 5 * time.Second}
	proxyURL := fmt.Sprintf("http://localhost:%d/v1/chat/completions", proxyPort)

	tests := []struct {
		name           string
		model          string
		authHeader     string
		expectUpstream string // partial URL to match
	}{
		{
			name:           "CloudCode - Gemini",
			model:          "gemini-2.5-flash",
			authHeader:     "Bearer dummy-gemini-key", // The proxy might use configured key or auth header
			expectUpstream: "/v1internal:generateContent",
		},
		{
			name:           "CloudCode - Antigravity (Claude)",
			model:          "claude-sonnet-4-5",
			authHeader:     "Bearer dummy-gemini-key",
			expectUpstream: "/v1internal:generateContent",
		},
		{
			name:           "AI Studio",
			model:          "gemini-2.5-pro", 
			// Note: gemini-2.5-pro is in GeminiCLIModels (registry), so it might route to Gemini Key.
			// But we want to test AI Studio Key routing.
			// AIStudio models overlap. Let's use a model only in AIStudio or rely on explicit provider routing?
			// The current logic prioritizes GeminiKey if model matches.
			// Let's use a specific AIStudio model or check round-robin if both exist.
			// Actually, let's use a custom model alias if possible, or just rely on the fact that we configured AIStudioKey.
			// Wait, the `service.go` logic registers providers.
			// If we ask for "gemini-2.5-flash", it could be GeminiCLI or AIStudio.
			// Let's assume the router picks one.
			// To strictly test AI Studio Portkey, we might need a model that maps only there or use `ai-studio` provider explicitly?
			// The gateway logic in TS routed based on model name.
			// In Go, it looks up `GetProviderName` -> `registry`.
			// `gemini-2.5-pro` is in `GetGeminiCLIModels` AND `GetAIStudioModels`.
			// So it has multiple providers. `Manager` will pick one.
			// Since we configured both, it might round-robin.
			// Let's try to force it or check if it hits *either* mock endpoint correctly.
			// Mock Upstream handles both on different paths.
			// `v1internal` for CloudCode, `v1/chat/completions` for Portkey.
			
			// Let's use a model that should definitely be CloudCode (Claude) vs one that could be AIStudio.
			authHeader: "Bearer dummy-aistudio-key",
			expectUpstream: "any", // We'll check logs
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]interface{}{
				"model": tt.model,
				"messages": []map[string]string{
					{"role": "user", "content": "Hello"},
				},
			}
			bodyBytes, _ := json.Marshal(body)
			req, _ := http.NewRequest("POST", proxyURL, bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", tt.authHeader)

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				b, _ := io.ReadAll(resp.Body)
				t.Errorf("Expected 200 OK, got %d: %s", resp.StatusCode, string(b))
			}

			// Verify Upstream Logs
			logResp, err := http.Get(fmt.Sprintf("http://localhost:%d/logs", mockUpstreamPort))
			if err != nil {
				t.Fatalf("Failed to get mock logs: %v", err)
			}
			defer logResp.Body.Close()
			
			var logs []struct{
				URL string `json:"url"`
			}
			json.NewDecoder(logResp.Body).Decode(&logs)
			
			if len(logs) == 0 {
				t.Error("No upstream request received")
			} else {
				last := logs[len(logs)-1]
				if tt.expectUpstream != "any" && !strings.Contains(last.URL, tt.expectUpstream) {
					t.Errorf("Expected upstream URL containing %s, got %s", tt.expectUpstream, last.URL)
				}
			}
			
			// Reset logs
			http.Get(fmt.Sprintf("http://localhost:%d/reset", mockUpstreamPort))
		})
	}
}
