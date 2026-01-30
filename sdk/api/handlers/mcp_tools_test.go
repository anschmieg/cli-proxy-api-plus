package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	internalmcp "github.com/router-for-me/CLIProxyAPI/v6/internal/mcp"
	"github.com/tidwall/gjson"
)

type fakeSession struct {
	tools []*mcp.Tool
}

func (f *fakeSession) ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	return &mcp.ListToolsResult{Tools: f.tools}, nil
}

func (f *fakeSession) CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{}, nil
}

func (f *fakeSession) Close() error { return nil }

func newFakeService(cfg *internalconfig.Config, tools []*mcp.Tool) *internalmcp.Service {
	return internalmcp.NewService(cfg, internalmcp.WithClientFactory(func(ctx context.Context, server internalconfig.MCPServer) (internalmcp.ClientSession, error) {
		return &fakeSession{tools: tools}, nil
	}))
}

func TestPrepareMCPForOpenAIInjectsTools(t *testing.T) {
	cfg := &internalconfig.Config{
		MCPServers: []internalconfig.MCPServer{
			{ID: "tools", Name: "Tools", Type: "local", Command: "echo"},
		},
	}
	handler := &BaseAPIHandler{FullCfg: cfg, MCPService: newFakeService(cfg, []*mcp.Tool{
		{Name: "search", Description: "Search", InputSchema: map[string]any{"type": "object"}},
	})}

	raw := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	updated, state, errMsg := handler.PrepareMCPForOpenAI(context.Background(), raw, nil)
	if errMsg != nil {
		t.Fatalf("unexpected error: %v", errMsg)
	}
	if state == nil || !state.HasTools() {
		t.Fatalf("expected MCP tools to be injected")
	}
	if got := gjson.GetBytes(updated, "tools.0.function.name").String(); got != "search" {
		t.Fatalf("expected injected tool name 'search', got %q", got)
	}
}

func TestPrepareMCPForClaudeMapsServerToolTypes(t *testing.T) {
	cfg := &internalconfig.Config{
		MCPServers: []internalconfig.MCPServer{
			{ID: "tools", Name: "Tools", Type: "local", Command: "echo"},
		},
		ServerToolMappings: []internalconfig.ServerToolMapping{
			{
				AnthropicToolType: "web_search_20250305",
				ToolName:          "web_search",
				MCPServerID:       "tools",
				MCPToolName:       "search",
			},
		},
	}

	inputSchema := map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}}
	tools := []*mcp.Tool{{Name: "search", Description: "Search", InputSchema: inputSchema}}

	handler := &BaseAPIHandler{FullCfg: cfg, MCPService: newFakeService(cfg, tools)}

	raw := []byte(`{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"web_search_20250305"}]}`)
	updated, state, errMsg := handler.PrepareMCPForClaude(context.Background(), raw, nil)
	if errMsg != nil {
		t.Fatalf("unexpected error: %v", errMsg)
	}
	if state == nil || !state.HasTools() {
		t.Fatalf("expected MCP tools to be detected")
	}
	if got := gjson.GetBytes(updated, "tools.0.name").String(); got != "web_search" {
		t.Fatalf("expected mapped tool name 'web_search', got %q", got)
	}
	schemaRaw := gjson.GetBytes(updated, "tools.0.input_schema").Raw
	var decoded map[string]any
	if err := json.Unmarshal([]byte(schemaRaw), &decoded); err != nil {
		t.Fatalf("failed to decode input_schema: %v", err)
	}
	if _, ok := decoded["properties"]; !ok {
		t.Fatalf("expected input_schema properties to be present")
	}
}
