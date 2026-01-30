package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

const defaultToolCacheTTL = 48 * time.Hour

var toolNameSuffixPattern = regexp.MustCompile(`-\d{4}-?\d{2}-?\d{2}$`)

type ToolDescriptor struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

type ClientSession interface {
	ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error)
	Close() error
}

type ClientFactory func(ctx context.Context, server config.MCPServer) (ClientSession, error)

type ServiceOption func(*Service)

type Service struct {
	mu            sync.RWMutex
	cfg           *config.Config
	cache         *toolCache
	cacheTTL      time.Duration
	clock         func() time.Time
	clientFactory ClientFactory
}

func NewService(cfg *config.Config, opts ...ServiceOption) *Service {
	service := &Service{
		cfg:           cfg,
		cache:         newToolCache(),
		cacheTTL:      defaultToolCacheTTL,
		clock:         time.Now,
		clientFactory: defaultClientFactory,
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func WithClientFactory(factory ClientFactory) ServiceOption {
	return func(service *Service) {
		if factory != nil {
			service.clientFactory = factory
		}
	}
}

func WithCacheTTL(ttl time.Duration) ServiceOption {
	return func(service *Service) {
		if ttl > 0 {
			service.cacheTTL = ttl
		}
	}
}

func WithClock(clock func() time.Time) ServiceOption {
	return func(service *Service) {
		if clock != nil {
			service.clock = clock
		}
	}
}

func (s *Service) UpdateConfig(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

func (s *Service) NormalizeToolName(name string) string {
	return NormalizeToolName(name)
}

func NormalizeToolName(name string) string {
	return toolNameSuffixPattern.ReplaceAllString(strings.TrimSpace(name), "")
}

func (s *Service) ResolveToolMappingByName(name string) *config.ServerToolMapping {
	if s == nil || strings.TrimSpace(name) == "" {
		return nil
	}
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	if cfg == nil || len(cfg.ServerToolMappings) == 0 {
		return nil
	}
	target := strings.ToLower(strings.TrimSpace(name))
	for i := range cfg.ServerToolMappings {
		entry := cfg.ServerToolMappings[i]
		if strings.ToLower(entry.ToolName) == target {
			return &entry
		}
	}
	return nil
}

func (s *Service) ResolveToolMappingByType(toolType string) *config.ServerToolMapping {
	if s == nil || strings.TrimSpace(toolType) == "" {
		return nil
	}
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	if cfg == nil || len(cfg.ServerToolMappings) == 0 {
		return nil
	}
	target := strings.ToLower(strings.TrimSpace(toolType))
	for i := range cfg.ServerToolMappings {
		entry := cfg.ServerToolMappings[i]
		if strings.ToLower(entry.AnthropicToolType) == target {
			return &entry
		}
	}
	return nil
}

func (s *Service) IsGatewayHandledTool(name string) bool {
	return s.ResolveToolMappingByName(name) != nil
}

func (s *Service) ResolveServer(id string) *config.MCPServer {
	if s == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	if cfg == nil {
		return nil
	}
	target := strings.ToLower(strings.TrimSpace(id))
	for i := range cfg.MCPServers {
		entry := cfg.MCPServers[i]
		if strings.ToLower(entry.ID) != target {
			continue
		}
		if entry.Enabled != nil && !*entry.Enabled {
			return nil
		}
		return &entry
	}
	return nil
}

func (s *Service) ListTools(ctx context.Context, serverID string) ([]ToolDescriptor, error) {
	server := s.ResolveServer(serverID)
	if server == nil {
		return nil, fmt.Errorf("mcp server %q not found or disabled", serverID)
	}
	if cached, ok := s.cache.get(server.ID, s.clock()); ok {
		return cached, nil
	}
	session, err := s.clientFactory(ctx, *server)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	tools := make([]ToolDescriptor, 0, len(result.Tools))
	for _, tool := range result.Tools {
		if tool == nil {
			continue
		}
		schema := json.RawMessage(`{}`)
		if tool.InputSchema != nil {
			if raw, err := json.Marshal(tool.InputSchema); err == nil {
				schema = raw
			}
		}
		description := tool.Description
		if description == "" && tool.Annotations != nil && tool.Annotations.Title != "" {
			description = tool.Annotations.Title
		}
		tools = append(tools, ToolDescriptor{
			Name:        tool.Name,
			Description: description,
			InputSchema: schema,
		})
	}
	s.cache.set(server.ID, tools, s.clock().Add(s.cacheTTL))
	return tools, nil
}

func (s *Service) ExecuteTool(ctx context.Context, toolName string, args map[string]any) (*mcp.CallToolResult, error) {
	resolved, err := s.resolveToolConfig(toolName)
	if err != nil {
		return nil, err
	}
	session, err := s.clientFactory(ctx, resolved.Server)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	actualTool := resolved.MCPToolName
	if actualTool == "" {
		actualTool = toolName
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      actualTool,
		Arguments: args,
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type resolvedTool struct {
	Server      config.MCPServer
	MCPToolName string
}

func (s *Service) resolveToolConfig(toolName string) (*resolvedTool, error) {
	if s == nil {
		return nil, errors.New("mcp service not configured")
	}
	trimmed := strings.TrimSpace(toolName)
	if trimmed == "" {
		return nil, errors.New("mcp tool name is required")
	}

	mapping := s.ResolveToolMappingByName(trimmed)
	if mapping == nil {
		normalized := NormalizeToolName(trimmed)
		if normalized != trimmed {
			mapping = s.ResolveToolMappingByName(normalized)
		}
	}
	if mapping != nil {
		server := s.ResolveServer(mapping.MCPServerID)
		if server == nil {
			return nil, fmt.Errorf("mcp server %q not found or disabled", mapping.MCPServerID)
		}
		return &resolvedTool{
			Server:      *server,
			MCPToolName: mapping.MCPToolName,
		}, nil
	}

	server := s.ResolveServer(trimmed)
	if server == nil {
		normalized := NormalizeToolName(trimmed)
		if normalized != trimmed {
			server = s.ResolveServer(normalized)
		}
	}
	if server == nil {
		return nil, fmt.Errorf("mcp server %q not found or disabled", trimmed)
	}
	return &resolvedTool{Server: *server}, nil
}

type toolCache struct {
	mu      sync.Mutex
	entries map[string]toolCacheEntry
}

type toolCacheEntry struct {
	tools   []ToolDescriptor
	expires time.Time
}

func newToolCache() *toolCache {
	return &toolCache{
		entries: make(map[string]toolCacheEntry),
	}
}

func (c *toolCache) get(key string, now time.Time) ([]ToolDescriptor, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || entry.expires.Before(now) {
		if ok {
			delete(c.entries, key)
		}
		return nil, false
	}
	out := make([]ToolDescriptor, len(entry.tools))
	copy(out, entry.tools)
	return out, true
}

func (c *toolCache) set(key string, tools []ToolDescriptor, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = toolCacheEntry{
		tools:   append([]ToolDescriptor(nil), tools...),
		expires: expires,
	}
}

func defaultClientFactory(ctx context.Context, server config.MCPServer) (ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "ai-gateway",
		Version: buildinfo.Version,
	}, nil)
	transport, err := buildTransport(server)
	if err != nil {
		return nil, err
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func buildTransport(server config.MCPServer) (mcp.Transport, error) {
	switch server.Type {
	case "local":
		cmd := exec.Command(server.Command, server.Args...)
		cmd.Env = mergeEnv(server.Env)
		return &mcp.CommandTransport{Command: cmd}, nil
	case "sse":
		if server.URL == "" {
			return nil, errors.New("mcp sse server url is required")
		}
		endpoint, err := url.Parse(server.URL)
		if err != nil {
			return nil, fmt.Errorf("invalid mcp sse url: %w", err)
		}
		return &mcp.SSEClientTransport{Endpoint: endpoint.String()}, nil
	default:
		return nil, fmt.Errorf("unsupported mcp server type %q", server.Type)
	}
}

func mergeEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
	env := append([]string{}, os.Environ()...)
	for key, value := range extra {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	return env
}
