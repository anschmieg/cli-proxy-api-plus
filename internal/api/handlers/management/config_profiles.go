package management

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// profiles: []Profile
func (h *Handler) GetProfiles(c *gin.Context) {
	c.JSON(200, gin.H{"profiles": h.cfg.Profiles})
}

func (h *Handler) PutProfiles(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.Profile
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.Profile `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	h.cfg.Profiles = arr
	h.cfg.SanitizeProfiles()
	h.persist(c)
}

func (h *Handler) PatchProfile(c *gin.Context) {
	type profilePatch struct {
		ID            *string              `json:"id"`
		Name          *string              `json:"name"`
		Description   *string              `json:"description"`
		DefaultModel  *string              `json:"default-model"`
		Tools         *config.ProfileTools `json:"tools"`
		KnowledgeBase *string              `json:"knowledge-base"`
		SystemPrompt  *string              `json:"system-prompt"`
		MCPServers    *[]string            `json:"mcp-servers"`
	}
	var body struct {
		ID    *string       `json:"id"`
		Index *int          `json:"index"`
		Value *profilePatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}

	targetIndex := -1
	if body.Index != nil && *body.Index >= 0 && *body.Index < len(h.cfg.Profiles) {
		targetIndex = *body.Index
	}
	if targetIndex == -1 && body.ID != nil {
		match := strings.TrimSpace(*body.ID)
		for i := range h.cfg.Profiles {
			if h.cfg.Profiles[i].ID == match {
				targetIndex = i
				break
			}
		}
	}
	if targetIndex == -1 {
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}

	entry := h.cfg.Profiles[targetIndex]
	if body.Value.ID != nil {
		entry.ID = strings.TrimSpace(*body.Value.ID)
	}
	if body.Value.Name != nil {
		entry.Name = strings.TrimSpace(*body.Value.Name)
	}
	if body.Value.Description != nil {
		entry.Description = strings.TrimSpace(*body.Value.Description)
	}
	if body.Value.DefaultModel != nil {
		entry.DefaultModel = strings.TrimSpace(*body.Value.DefaultModel)
	}
	if body.Value.Tools != nil {
		entry.Tools = *body.Value.Tools
	}
	if body.Value.KnowledgeBase != nil {
		entry.KnowledgeBase = strings.TrimSpace(*body.Value.KnowledgeBase)
	}
	if body.Value.SystemPrompt != nil {
		entry.SystemPrompt = strings.TrimSpace(*body.Value.SystemPrompt)
	}
	if body.Value.MCPServers != nil {
		entry.MCPServers = append([]string(nil), (*body.Value.MCPServers)...)
	}
	h.cfg.Profiles[targetIndex] = entry
	h.cfg.SanitizeProfiles()
	h.persist(c)
}

func (h *Handler) DeleteProfile(c *gin.Context) {
	if id := strings.TrimSpace(c.Query("id")); id != "" {
		out := make([]config.Profile, 0, len(h.cfg.Profiles))
		for _, v := range h.cfg.Profiles {
			if v.ID != id {
				out = append(out, v)
			}
		}
		h.cfg.Profiles = out
		h.cfg.SanitizeProfiles()
		h.persist(c)
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		_, err := fmt.Sscanf(idxStr, "%d", &idx)
		if err == nil && idx >= 0 && idx < len(h.cfg.Profiles) {
			h.cfg.Profiles = append(h.cfg.Profiles[:idx], h.cfg.Profiles[idx+1:]...)
			h.cfg.SanitizeProfiles()
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing id or index"})
}

// mcp-servers: []MCPServer
func (h *Handler) GetMCPServers(c *gin.Context) {
	c.JSON(200, gin.H{"mcp-servers": h.cfg.MCPServers})
}

func (h *Handler) PutMCPServers(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.MCPServer
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.MCPServer `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	h.cfg.MCPServers = arr
	h.cfg.SanitizeMCPServers()
	h.persist(c)
}

func (h *Handler) PatchMCPServer(c *gin.Context) {
	type mcpServerPatch struct {
		ID      *string            `json:"id"`
		Name    *string            `json:"name"`
		Type    *string            `json:"type"`
		Command *string            `json:"command"`
		Args    *[]string          `json:"args"`
		Env     *map[string]string `json:"env"`
		URL     *string            `json:"url"`
		Enabled *bool              `json:"enabled"`
	}
	var body struct {
		ID    *string         `json:"id"`
		Index *int            `json:"index"`
		Value *mcpServerPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	targetIndex := -1
	if body.Index != nil && *body.Index >= 0 && *body.Index < len(h.cfg.MCPServers) {
		targetIndex = *body.Index
	}
	if targetIndex == -1 && body.ID != nil {
		match := strings.TrimSpace(*body.ID)
		for i := range h.cfg.MCPServers {
			if h.cfg.MCPServers[i].ID == match {
				targetIndex = i
				break
			}
		}
	}
	if targetIndex == -1 {
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}

	entry := h.cfg.MCPServers[targetIndex]
	if body.Value.ID != nil {
		entry.ID = strings.TrimSpace(*body.Value.ID)
	}
	if body.Value.Name != nil {
		entry.Name = strings.TrimSpace(*body.Value.Name)
	}
	if body.Value.Type != nil {
		entry.Type = strings.TrimSpace(*body.Value.Type)
	}
	if body.Value.Command != nil {
		entry.Command = strings.TrimSpace(*body.Value.Command)
	}
	if body.Value.Args != nil {
		entry.Args = append([]string(nil), (*body.Value.Args)...)
	}
	if body.Value.Env != nil {
		entry.Env = make(map[string]string, len(*body.Value.Env))
		for k, v := range *body.Value.Env {
			entry.Env[k] = v
		}
	}
	if body.Value.URL != nil {
		entry.URL = strings.TrimSpace(*body.Value.URL)
	}
	if body.Value.Enabled != nil {
		val := *body.Value.Enabled
		entry.Enabled = &val
	}
	h.cfg.MCPServers[targetIndex] = entry
	h.cfg.SanitizeMCPServers()
	h.persist(c)
}

func (h *Handler) DeleteMCPServer(c *gin.Context) {
	if id := strings.TrimSpace(c.Query("id")); id != "" {
		out := make([]config.MCPServer, 0, len(h.cfg.MCPServers))
		for _, v := range h.cfg.MCPServers {
			if v.ID != id {
				out = append(out, v)
			}
		}
		h.cfg.MCPServers = out
		h.cfg.SanitizeMCPServers()
		h.persist(c)
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		_, err := fmt.Sscanf(idxStr, "%d", &idx)
		if err == nil && idx >= 0 && idx < len(h.cfg.MCPServers) {
			h.cfg.MCPServers = append(h.cfg.MCPServers[:idx], h.cfg.MCPServers[idx+1:]...)
			h.cfg.SanitizeMCPServers()
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing id or index"})
}

// tool-mappings: []ServerToolMapping
func (h *Handler) GetToolMappings(c *gin.Context) {
	c.JSON(200, gin.H{"tool-mappings": h.cfg.ServerToolMappings})
}

func (h *Handler) PutToolMappings(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.ServerToolMapping
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.ServerToolMapping `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	h.cfg.ServerToolMappings = arr
	h.cfg.SanitizeToolMappings()
	h.persist(c)
}

func (h *Handler) PatchToolMapping(c *gin.Context) {
	type toolMappingPatch struct {
		AnthropicToolType *string `json:"anthropic-tool-type"`
		ToolName          *string `json:"tool-name"`
		MCPServerID       *string `json:"mcp-server-id"`
		MCPToolName       *string `json:"mcp-tool-name"`
	}
	var body struct {
		ToolName *string           `json:"tool-name"`
		Index    *int              `json:"index"`
		Value    *toolMappingPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	targetIndex := -1
	if body.Index != nil && *body.Index >= 0 && *body.Index < len(h.cfg.ServerToolMappings) {
		targetIndex = *body.Index
	}
	if targetIndex == -1 && body.ToolName != nil {
		match := strings.TrimSpace(*body.ToolName)
		for i := range h.cfg.ServerToolMappings {
			if h.cfg.ServerToolMappings[i].ToolName == match {
				targetIndex = i
				break
			}
		}
	}
	if targetIndex == -1 {
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}

	entry := h.cfg.ServerToolMappings[targetIndex]
	if body.Value.AnthropicToolType != nil {
		entry.AnthropicToolType = strings.TrimSpace(*body.Value.AnthropicToolType)
	}
	if body.Value.ToolName != nil {
		entry.ToolName = strings.TrimSpace(*body.Value.ToolName)
	}
	if body.Value.MCPServerID != nil {
		entry.MCPServerID = strings.TrimSpace(*body.Value.MCPServerID)
	}
	if body.Value.MCPToolName != nil {
		entry.MCPToolName = strings.TrimSpace(*body.Value.MCPToolName)
	}
	h.cfg.ServerToolMappings[targetIndex] = entry
	h.cfg.SanitizeToolMappings()
	h.persist(c)
}

func (h *Handler) DeleteToolMapping(c *gin.Context) {
	if toolName := strings.TrimSpace(c.Query("tool-name")); toolName != "" {
		out := make([]config.ServerToolMapping, 0, len(h.cfg.ServerToolMappings))
		for _, v := range h.cfg.ServerToolMappings {
			if v.ToolName != toolName {
				out = append(out, v)
			}
		}
		h.cfg.ServerToolMappings = out
		h.cfg.SanitizeToolMappings()
		h.persist(c)
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		_, err := fmt.Sscanf(idxStr, "%d", &idx)
		if err == nil && idx >= 0 && idx < len(h.cfg.ServerToolMappings) {
			h.cfg.ServerToolMappings = append(h.cfg.ServerToolMappings[:idx], h.cfg.ServerToolMappings[idx+1:]...)
			h.cfg.SanitizeToolMappings()
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing tool-name or index"})
}
