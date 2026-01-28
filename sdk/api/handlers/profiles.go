package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/constant"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ApplyProfileToPayload applies profile-driven routing and payload updates.
// It supports model@profile suffixes, profile-as-model default routing, system prompts,
// and tool allow/deny policies.
func (h *BaseAPIHandler) ApplyProfileToPayload(ctx context.Context, handlerType string, rawJSON []byte) ([]byte, *interfaces.ErrorMessage) {
	if h == nil || h.FullCfg == nil || len(h.FullCfg.Profiles) == 0 {
		return rawJSON, nil
	}
	modelName := strings.TrimSpace(gjson.GetBytes(rawJSON, "model").String())
	if modelName == "" {
		return rawJSON, nil
	}

	profile, upstreamModel, errMsg := resolveProfileFromModel(modelName, h.FullCfg.Profiles)
	if errMsg != nil || profile == nil {
		return rawJSON, errMsg
	}

	updated := rawJSON
	if upstreamModel != "" && upstreamModel != modelName {
		if next, err := sjson.SetBytes(updated, "model", upstreamModel); err == nil {
			updated = next
		}
	}

	if prompt := strings.TrimSpace(profile.SystemPrompt); prompt != "" {
		if handlerType == constant.Claude {
			updated = applyClaudeSystemPrompt(updated, prompt)
		} else {
			updated = applyOpenAISystemPrompt(updated, prompt)
		}
	}

	if kb := strings.TrimSpace(profile.KnowledgeBase); kb != "" {
		updated = applyKnowledgeBase(ctx, handlerType, updated, kb, h)
	}

	updated = applyProfileTools(updated, profile.Tools)
	return updated, nil
}

func resolveProfileFromModel(modelName string, profiles []internalconfig.Profile) (*internalconfig.Profile, string, *interfaces.ErrorMessage) {
	if modelName == "" || len(profiles) == 0 {
		return nil, "", nil
	}
	trimmed := strings.TrimSpace(modelName)
	if trimmed == "" {
		return nil, "", nil
	}

	if at := strings.LastIndex(trimmed, "@"); at > 0 && at < len(trimmed)-1 {
		candidateID := strings.TrimSpace(trimmed[at+1:])
		if candidateID != "" {
			for i := range profiles {
				if profiles[i].ID == candidateID {
					upstream := strings.TrimSpace(trimmed[:at])
					if upstream == "" {
						return nil, "", &interfaces.ErrorMessage{
							StatusCode: http.StatusBadRequest,
							Error:      fmt.Errorf("Profile '%s' requires a base model. Use 'model@profile'.", profiles[i].ID),
						}
					}
					return &profiles[i], upstream, nil
				}
			}
		}
	}

	for i := range profiles {
		if profiles[i].ID == trimmed {
			defaultModel := strings.TrimSpace(profiles[i].DefaultModel)
			if defaultModel == "" {
				return nil, "", &interfaces.ErrorMessage{
					StatusCode: http.StatusBadRequest,
					Error:      fmt.Errorf("Profile '%s' has no default model configured. Please specify 'model@profile'.", profiles[i].ID),
				}
			}
			return &profiles[i], defaultModel, nil
		}
	}

	return nil, "", nil
}

func applyOpenAISystemPrompt(rawJSON []byte, prompt string) []byte {
	messages := gjson.GetBytes(rawJSON, "messages")
	if messages.Exists() && messages.IsArray() {
		msgJSON, _ := json.Marshal(prompt)
		systemMessage := fmt.Sprintf(`{"role":"system","content":%s}`, msgJSON)

		var builder strings.Builder
		builder.WriteString("[")
		builder.WriteString(systemMessage)
		messages.ForEach(func(_, item gjson.Result) bool {
			builder.WriteString(",")
			builder.WriteString(item.Raw)
			return true
		})
		builder.WriteString("]")

		if updated, err := sjson.SetRawBytes(rawJSON, "messages", []byte(builder.String())); err == nil {
			return updated
		}
		return rawJSON
	}

	if instructions := gjson.GetBytes(rawJSON, "instructions"); instructions.Exists() && instructions.Type == gjson.String {
		existing := strings.TrimSpace(instructions.String())
		merged := prompt
		if existing != "" {
			merged = prompt + "\n\n" + existing
		}
		if updated, err := sjson.SetBytes(rawJSON, "instructions", merged); err == nil {
			return updated
		}
		return rawJSON
	}

	if !gjson.GetBytes(rawJSON, "instructions").Exists() && gjson.GetBytes(rawJSON, "input").Exists() {
		if updated, err := sjson.SetBytes(rawJSON, "instructions", prompt); err == nil {
			return updated
		}
	}
	return rawJSON
}

func applyClaudeSystemPrompt(rawJSON []byte, prompt string) []byte {
	system := gjson.GetBytes(rawJSON, "system")
	if !system.Exists() {
		if updated, err := sjson.SetBytes(rawJSON, "system", prompt); err == nil {
			return updated
		}
		return rawJSON
	}

	if system.Type == gjson.String {
		existing := strings.TrimSpace(system.String())
		merged := prompt
		if existing != "" {
			merged = prompt + "\n\n" + existing
		}
		if updated, err := sjson.SetBytes(rawJSON, "system", merged); err == nil {
			return updated
		}
		return rawJSON
	}

	if system.IsArray() {
		msgJSON, _ := json.Marshal(prompt)
		systemMessage := fmt.Sprintf(`{"type":"text","text":%s}`, msgJSON)
		var builder strings.Builder
		builder.WriteString("[")
		builder.WriteString(systemMessage)
		system.ForEach(func(_, item gjson.Result) bool {
			builder.WriteString(",")
			builder.WriteString(item.Raw)
			return true
		})
		builder.WriteString("]")
		if updated, err := sjson.SetRawBytes(rawJSON, "system", []byte(builder.String())); err == nil {
			return updated
		}
	}

	return rawJSON
}

func applyProfileTools(rawJSON []byte, tools internalconfig.ProfileTools) []byte {
	if tools.Enabled != nil && !*tools.Enabled {
		return stripTools(rawJSON)
	}

	if len(tools.Allowlist) == 0 {
		return rawJSON
	}

	allow := make(map[string]struct{}, len(tools.Allowlist))
	for _, name := range tools.Allowlist {
		name = strings.TrimSpace(name)
		if name != "" {
			allow[name] = struct{}{}
		}
	}
	if len(allow) == 0 {
		return rawJSON
	}

	updated := rawJSON

	if toolsResult := gjson.GetBytes(updated, "tools"); toolsResult.Exists() && toolsResult.IsArray() {
		var builder strings.Builder
		kept := 0
		builder.WriteString("[")
		toolsResult.ForEach(func(_, item gjson.Result) bool {
			name := strings.TrimSpace(extractToolName(item))
			if name == "" {
				return true
			}
			if _, ok := allow[name]; ok {
				if kept > 0 {
					builder.WriteString(",")
				}
				builder.WriteString(item.Raw)
				kept++
			}
			return true
		})
		builder.WriteString("]")

		if kept == 0 {
			updated = stripTools(updated)
		} else if next, err := sjson.SetRawBytes(updated, "tools", []byte(builder.String())); err == nil {
			updated = next
			if choice := resolveToolChoiceName(updated); choice != "" {
				if _, ok := allow[choice]; !ok {
					updated = deleteToolChoice(updated)
				}
			}
		}
	}

	if functions := gjson.GetBytes(updated, "functions"); functions.Exists() && functions.IsArray() {
		var builder strings.Builder
		kept := 0
		builder.WriteString("[")
		functions.ForEach(func(_, item gjson.Result) bool {
			name := strings.TrimSpace(item.Get("name").String())
			if name == "" {
				return true
			}
			if _, ok := allow[name]; ok {
				if kept > 0 {
					builder.WriteString(",")
				}
				builder.WriteString(item.Raw)
				kept++
			}
			return true
		})
		builder.WriteString("]")
		if kept == 0 {
			if next, err := sjson.DeleteBytes(updated, "functions"); err == nil {
				updated = next
			}
			if next, err := sjson.DeleteBytes(updated, "function_call"); err == nil {
				updated = next
			}
		} else if next, err := sjson.SetRawBytes(updated, "functions", []byte(builder.String())); err == nil {
			updated = next
		}
	}

	return updated
}

func stripTools(rawJSON []byte) []byte {
	updated := rawJSON
	if next, err := sjson.DeleteBytes(updated, "tools"); err == nil {
		updated = next
	}
	if next, err := sjson.DeleteBytes(updated, "tool_choice"); err == nil {
		updated = next
	}
	if next, err := sjson.DeleteBytes(updated, "functions"); err == nil {
		updated = next
	}
	if next, err := sjson.DeleteBytes(updated, "function_call"); err == nil {
		updated = next
	}
	return updated
}

func deleteToolChoice(rawJSON []byte) []byte {
	if updated, err := sjson.DeleteBytes(rawJSON, "tool_choice"); err == nil {
		return updated
	}
	return rawJSON
}

func resolveToolChoiceName(rawJSON []byte) string {
	toolChoice := gjson.GetBytes(rawJSON, "tool_choice")
	if !toolChoice.Exists() {
		return ""
	}
	switch toolChoice.Type {
	case gjson.String:
		val := strings.TrimSpace(toolChoice.String())
		switch val {
		case "", "auto", "none":
			return ""
		default:
			return val
		}
	case gjson.JSON:
		if name := strings.TrimSpace(toolChoice.Get("function.name").String()); name != "" {
			return name
		}
		if name := strings.TrimSpace(toolChoice.Get("name").String()); name != "" {
			return name
		}
	}
	return ""
}

func extractToolName(tool gjson.Result) string {
	if name := strings.TrimSpace(tool.Get("name").String()); name != "" {
		return name
	}
	if name := strings.TrimSpace(tool.Get("function.name").String()); name != "" {
		return name
	}
	return ""
}
