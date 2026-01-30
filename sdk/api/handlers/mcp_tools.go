package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type MCPToolState struct {
	ToolNames          map[string]struct{}
	ServerToolMappings map[string]string
}

func (s *MCPToolState) HasTools() bool {
	return s != nil && len(s.ToolNames) > 0
}

func (h *BaseAPIHandler) ResolveProfileForPayload(rawJSON []byte) (*internalconfig.Profile, string, *interfaces.ErrorMessage) {
	if h == nil || h.FullCfg == nil || len(h.FullCfg.Profiles) == 0 {
		return nil, "", nil
	}
	modelName := strings.TrimSpace(gjson.GetBytes(rawJSON, "model").String())
	if modelName == "" {
		return nil, "", nil
	}
	return resolveProfileFromModel(modelName, h.FullCfg.Profiles)
}

func (h *BaseAPIHandler) PrepareMCPForOpenAI(ctx context.Context, rawJSON []byte, profile *internalconfig.Profile) ([]byte, *MCPToolState, *interfaces.ErrorMessage) {
	if h == nil || h.FullCfg == nil || h.MCPService == nil {
		return rawJSON, nil, nil
	}
	if !profileAllowsTools(profile) {
		return rawJSON, nil, nil
	}
	if len(h.FullCfg.MCPServers) == 0 {
		return rawJSON, nil, nil
	}

	allowedServers := profileServerAllowlist(profile)
	allowedTools := profileToolAllowlist(profile)

	toolsResult := gjson.GetBytes(rawJSON, "tools")
	var tools []string
	existingNames := map[string]struct{}{}
	if toolsResult.Exists() && toolsResult.IsArray() {
		toolsResult.ForEach(func(_, tool gjson.Result) bool {
			if name := strings.TrimSpace(extractToolName(tool)); name != "" {
				existingNames[name] = struct{}{}
			}
			tools = append(tools, tool.Raw)
			return true
		})
	}

	added := 0
	toolNames := map[string]struct{}{}
	for _, server := range h.FullCfg.MCPServers {
		if server.Enabled != nil && !*server.Enabled {
			continue
		}
		if len(allowedServers) > 0 {
			if _, ok := allowedServers[strings.ToLower(server.ID)]; !ok {
				continue
			}
		}
		toolList, err := h.MCPService.ListTools(ctx, server.ID)
		if err != nil {
			log.WithError(err).Warnf("mcp: failed to list tools for server %s", server.ID)
			continue
		}
		for _, tool := range toolList {
			name := strings.TrimSpace(tool.Name)
			if name == "" {
				continue
			}
			if _, exists := existingNames[name]; exists {
				continue
			}
			if len(allowedTools) > 0 {
				if _, ok := allowedTools[name]; !ok {
					continue
				}
			}
			toolPayload := map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        name,
					"description": tool.Description,
					"parameters":  json.RawMessage(tool.InputSchema),
				},
			}
			rawTool, err := json.Marshal(toolPayload)
			if err != nil {
				log.WithError(err).Warnf("mcp: failed to marshal tool %s payload", name)
				continue
			}
			tools = append(tools, string(rawTool))
			existingNames[name] = struct{}{}
			toolNames[name] = struct{}{}
			added++
		}
	}

	if added == 0 {
		return rawJSON, &MCPToolState{ToolNames: toolNames}, nil
	}

	var builder strings.Builder
	builder.WriteString("[")
	for i, tool := range tools {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(tool)
	}
	builder.WriteString("]")

	updated, err := sjson.SetRawBytes(rawJSON, "tools", []byte(builder.String()))
	if err != nil {
		return rawJSON, nil, &interfaces.ErrorMessage{
			StatusCode: 500,
			Error:      fmt.Errorf("failed to inject MCP tools"),
		}
	}
	return updated, &MCPToolState{ToolNames: toolNames}, nil
}

func (h *BaseAPIHandler) PrepareMCPForClaude(ctx context.Context, rawJSON []byte, profile *internalconfig.Profile) ([]byte, *MCPToolState, *interfaces.ErrorMessage) {
	if h == nil || h.FullCfg == nil || h.MCPService == nil {
		return rawJSON, nil, nil
	}
	if !profileAllowsTools(profile) {
		return rawJSON, nil, nil
	}

	allowedServers := profileServerAllowlist(profile)
	allowedTools := profileToolAllowlist(profile)

	toolsResult := gjson.GetBytes(rawJSON, "tools")
	if !toolsResult.Exists() || !toolsResult.IsArray() {
		return rawJSON, nil, nil
	}

	serverToolMappings := map[string]string{}
	toolNames := map[string]struct{}{}

	var tools []string
	toolsResult.ForEach(func(_, tool gjson.Result) bool {
		toolType := strings.TrimSpace(tool.Get("type").String())
		name := strings.TrimSpace(tool.Get("name").String())

		if toolType != "" && toolType != "function" && toolType != "custom" {
			mapping := h.MCPService.ResolveToolMappingByType(toolType)
			if mapping != nil && mapping.ToolName != "" {
				if len(allowedServers) > 0 {
					if _, ok := allowedServers[strings.ToLower(mapping.MCPServerID)]; !ok {
						return true
					}
				}
				if len(allowedTools) > 0 {
					if _, ok := allowedTools[mapping.ToolName]; !ok {
						return true
					}
				}
				toolSchema := json.RawMessage(`{}`)
				if mapping.MCPServerID != "" && mapping.MCPToolName != "" {
					if toolList, err := h.MCPService.ListTools(ctx, mapping.MCPServerID); err == nil {
						for _, candidate := range toolList {
							if candidate.Name == mapping.MCPToolName {
								toolSchema = candidate.InputSchema
								break
							}
						}
					}
				}

				toolPayload := map[string]any{
					"name":         mapping.ToolName,
					"description":  fmt.Sprintf("Server-side tool: %s", toolType),
					"input_schema": json.RawMessage(toolSchema),
				}
				rawTool, err := json.Marshal(toolPayload)
				if err == nil {
					tools = append(tools, string(rawTool))
					toolNames[mapping.ToolName] = struct{}{}
					serverToolMappings[mapping.ToolName] = mapping.MCPToolName
					return true
				}
				log.WithError(err).Warnf("mcp: failed to marshal tool mapping for %s", mapping.ToolName)
			}
		}

		if name != "" {
			if mapping := h.MCPService.ResolveToolMappingByName(name); mapping != nil {
				serverAllowed := true
				if len(allowedServers) > 0 {
					_, serverAllowed = allowedServers[strings.ToLower(mapping.MCPServerID)]
				}
				toolAllowed := true
				if len(allowedTools) > 0 {
					_, toolAllowed = allowedTools[name]
				}
				if serverAllowed && toolAllowed {
					toolNames[name] = struct{}{}
					serverToolMappings[name] = mapping.MCPToolName
				}
			}
		}

		tools = append(tools, tool.Raw)
		return true
	})

	if len(tools) == 0 {
		return rawJSON, nil, nil
	}

	var builder strings.Builder
	builder.WriteString("[")
	for i, tool := range tools {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(tool)
	}
	builder.WriteString("]")

	updated, err := sjson.SetRawBytes(rawJSON, "tools", []byte(builder.String()))
	if err != nil {
		return rawJSON, nil, &interfaces.ErrorMessage{
			StatusCode: 500,
			Error:      fmt.Errorf("failed to update tool mappings"),
		}
	}

	state := &MCPToolState{
		ToolNames:          toolNames,
		ServerToolMappings: serverToolMappings,
	}
	return updated, state, nil
}

func profileAllowsTools(profile *internalconfig.Profile) bool {
	if profile == nil {
		return true
	}
	if profile.Tools.Enabled != nil && !*profile.Tools.Enabled {
		return false
	}
	return true
}

func profileToolAllowlist(profile *internalconfig.Profile) map[string]struct{} {
	if profile == nil || len(profile.Tools.Allowlist) == 0 {
		return nil
	}
	allow := make(map[string]struct{}, len(profile.Tools.Allowlist))
	for _, name := range profile.Tools.Allowlist {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			allow[trimmed] = struct{}{}
		}
	}
	if len(allow) == 0 {
		return nil
	}
	return allow
}

func profileServerAllowlist(profile *internalconfig.Profile) map[string]struct{} {
	if profile == nil || len(profile.MCPServers) == 0 {
		return nil
	}
	allow := make(map[string]struct{}, len(profile.MCPServers))
	for _, id := range profile.MCPServers {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			allow[strings.ToLower(trimmed)] = struct{}{}
		}
	}
	if len(allow) == 0 {
		return nil
	}
	return allow
}
