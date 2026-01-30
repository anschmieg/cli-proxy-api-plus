package handlers_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	internalmcp "github.com/router-for-me/CLIProxyAPI/v6/internal/mcp"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	claudehandlers "github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers/claude"
	openaihandlers "github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers/openai"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
)

type sequenceExecutor struct {
	identifier string
	responses  [][]byte
	mu         sync.Mutex
	requests   []coreexecutor.Request
}

func (e *sequenceExecutor) Identifier() string { return e.identifier }

func (e *sequenceExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.requests = append(e.requests, req)
	idx := len(e.requests) - 1
	if idx >= len(e.responses) {
		return coreexecutor.Response{Payload: []byte(`{}`)}, nil
	}
	return coreexecutor.Response{Payload: e.responses[idx]}, nil
}

func (e *sequenceExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (<-chan coreexecutor.StreamChunk, error) {
	return nil, &coreauth.Error{Code: "not_implemented", Message: "stream not implemented"}
}

func (e *sequenceExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *sequenceExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{Code: "not_implemented", Message: "CountTokens not implemented"}
}

func (e *sequenceExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{Code: "not_implemented", Message: "HttpRequest not implemented", HTTPStatus: http.StatusNotImplemented}
}

func (e *sequenceExecutor) Requests() []coreexecutor.Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]coreexecutor.Request, len(e.requests))
	copy(out, e.requests)
	return out
}

type mcpTestState struct {
	mu        sync.Mutex
	tools     []*mcp.Tool
	callArgs  []mcp.CallToolParams
	callCount int
}

func (s *mcpTestState) factory(context.Context, internalconfig.MCPServer) (internalmcp.ClientSession, error) {
	return &mcpTestSession{state: s}, nil
}

func (s *mcpTestState) Calls() []mcp.CallToolParams {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]mcp.CallToolParams, len(s.callArgs))
	copy(out, s.callArgs)
	return out
}

type mcpTestSession struct {
	state *mcpTestState
}

func (s *mcpTestSession) ListTools(context.Context, *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	tools := make([]*mcp.Tool, len(s.state.tools))
	copy(tools, s.state.tools)
	return &mcp.ListToolsResult{Tools: tools}, nil
}

func (s *mcpTestSession) CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.state.callCount++
	s.state.callArgs = append(s.state.callArgs, *params)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
	}, nil
}

func (s *mcpTestSession) Close() error { return nil }

func TestOpenAIChatCompletions_MCPToolLoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &internalconfig.Config{
		MCPServers: []internalconfig.MCPServer{
			{ID: "server-1", Type: "local", Command: "noop"},
		},
		ServerToolMappings: []internalconfig.ServerToolMapping{
			{ToolName: "toolA", MCPServerID: "server-1", MCPToolName: "toolA"},
		},
	}

	manager := coreauth.NewManager(nil, nil, nil)
	executor := &sequenceExecutor{
		identifier: "openai",
		responses: [][]byte{
			[]byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":"toolA","arguments":"{\"query\":\"hi\"}"}}]}}]}`),
			[]byte(`{"id":"chatcmpl-2","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"done"}}]}`),
		},
	}
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{ID: "auth-openai", Provider: "openai", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	mcpState := &mcpTestState{
		tools: []*mcp.Tool{
			{Name: "toolA", Description: "tool A", InputSchema: map[string]any{"type": "object"}},
		},
	}

	base := handlers.NewBaseAPIHandlersWithConfig(cfg, manager)
	base.MCPService = internalmcp.NewService(cfg, internalmcp.WithClientFactory(mcpState.factory))
	api := openaihandlers.NewOpenAIAPIHandler(base)

	router := gin.New()
	router.POST("/v1/chat/completions", api.ChatCompletions)

	body := []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !gjson.GetBytes(rec.Body.Bytes(), "choices.0.message.content").Exists() {
		t.Fatalf("expected response content, got %s", rec.Body.String())
	}

	requests := executor.Requests()
	if len(requests) < 2 {
		t.Fatalf("expected 2 upstream calls, got %d", len(requests))
	}
	if !hasOpenAIToolResult(requests[1].Payload, "call-1") {
		t.Fatalf("expected tool result message in second request payload")
	}

	calls := mcpState.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 MCP tool call, got %d", len(calls))
	}
	if calls[0].Name != "toolA" {
		t.Fatalf("expected toolA call, got %s", calls[0].Name)
	}
}

func TestClaudeMessages_MCPToolLoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &internalconfig.Config{
		MCPServers: []internalconfig.MCPServer{
			{ID: "server-1", Type: "local", Command: "noop"},
		},
		ServerToolMappings: []internalconfig.ServerToolMapping{
			{ToolName: "toolA", MCPServerID: "server-1", MCPToolName: "toolA"},
		},
	}

	manager := coreauth.NewManager(nil, nil, nil)
	executor := &sequenceExecutor{
		identifier: "claude",
		responses: [][]byte{
			[]byte(`{"id":"msg-1","type":"message","role":"assistant","model":"test-model","content":[{"type":"tool_use","id":"tool-1","name":"toolA","input":{"query":"hi"}}],"stop_reason":"tool_use"}`),
			[]byte(`{"id":"msg-2","type":"message","role":"assistant","model":"test-model","content":[{"type":"text","text":"done"}]}`),
		},
	}
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{ID: "auth-claude", Provider: "claude", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	mcpState := &mcpTestState{
		tools: []*mcp.Tool{
			{Name: "toolA", Description: "tool A", InputSchema: map[string]any{"type": "object"}},
		},
	}

	base := handlers.NewBaseAPIHandlersWithConfig(cfg, manager)
	base.MCPService = internalmcp.NewService(cfg, internalmcp.WithClientFactory(mcpState.factory))
	api := claudehandlers.NewClaudeCodeAPIHandler(base)

	router := gin.New()
	router.POST("/v1/messages", api.ClaudeMessages)

	body := []byte(`{"model":"test-model","max_tokens":16,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"toolA","description":"tool A","input_schema":{"type":"object"}}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !gjson.GetBytes(rec.Body.Bytes(), "content.0.text").Exists() {
		t.Fatalf("expected response content, got %s", rec.Body.String())
	}

	requests := executor.Requests()
	if len(requests) < 2 {
		t.Fatalf("expected 2 upstream calls, got %d", len(requests))
	}
	if !hasClaudeToolResult(requests[1].Payload, "tool-1") {
		t.Fatalf("expected tool result message in second request payload")
	}

	calls := mcpState.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 MCP tool call, got %d", len(calls))
	}
	if calls[0].Name != "toolA" {
		t.Fatalf("expected toolA call, got %s", calls[0].Name)
	}
}

func hasOpenAIToolResult(payload []byte, callID string) bool {
	messages := gjson.GetBytes(payload, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return false
	}
	found := false
	messages.ForEach(func(_, msg gjson.Result) bool {
		if msg.Get("role").String() == "tool" && msg.Get("tool_call_id").String() == callID {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasClaudeToolResult(payload []byte, toolUseID string) bool {
	messages := gjson.GetBytes(payload, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return false
	}
	found := false
	messages.ForEach(func(_, msg gjson.Result) bool {
		if msg.Get("role").String() != "user" {
			return true
		}
		content := msg.Get("content")
		if !content.Exists() || !content.IsArray() {
			return true
		}
		content.ForEach(func(_, block gjson.Result) bool {
			if block.Get("type").String() == "tool_result" && block.Get("tool_use_id").String() == toolUseID {
				found = true
				return false
			}
			return true
		})
		return !found
	})
	return found
}
