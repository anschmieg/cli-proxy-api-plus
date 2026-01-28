package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/constant"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/knowledge"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const ragSystemPrompt = `You are a helpful assistant with access to a knowledge base. When answering questions, use the provided context from the knowledge base to give accurate and relevant responses. If the context is helpful, cite the source. If the context doesn't help answer the question, you may say so and respond based on your general knowledge.`

func applyKnowledgeBase(ctx context.Context, handlerType string, rawJSON []byte, project string, h *BaseAPIHandler) []byte {
	if h == nil || h.KnowledgeManager == nil || h.FullCfg == nil {
		return rawJSON
	}
	if !h.FullCfg.Knowledge.Enabled {
		return rawJSON
	}
	query := extractLastUserQuery(rawJSON)
	if query == "" {
		return rawJSON
	}
	if ctx == nil {
		ctx = context.Background()
	}

	searchCfg := h.FullCfg.Knowledge.Search
	limit := searchCfg.Limit
	if limit <= 0 {
		limit = 5
	}

	docs, err := h.KnowledgeManager.Query(ctx, query, limit, map[string]string{"project": project})
	if err != nil || len(docs) == 0 {
		return rawJSON
	}

	contextText, sources, avgScore := buildKnowledgeContext(docs, searchCfg)
	if contextText == "" {
		return rawJSON
	}
	if avgScore < searchCfg.MinScore {
		return rawJSON
	}

	message := fmt.Sprintf(
		"%s\n\n<knowledge_base project=\"%s\">\n%s\n</knowledge_base>\n\nSources available: %s",
		ragSystemPrompt,
		project,
		contextText,
		strings.Join(sources, ", "),
	)

	if handlerType == constant.Claude {
		return injectClaudeRAGContext(rawJSON, message)
	}
	return injectOpenAIRAGContext(rawJSON, message)
}

func extractLastUserQuery(rawJSON []byte) string {
	messages := gjson.GetBytes(rawJSON, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return ""
	}
	items := messages.Array()
	for i := len(items) - 1; i >= 0; i-- {
		if strings.EqualFold(items[i].Get("role").String(), "user") {
			return strings.TrimSpace(extractMessageText(items[i].Get("content")))
		}
	}
	return ""
}

func extractMessageText(content gjson.Result) string {
	switch {
	case content.Type == gjson.String:
		return content.String()
	case content.IsArray():
		parts := make([]string, 0)
		content.ForEach(func(_, item gjson.Result) bool {
			typ := strings.ToLower(strings.TrimSpace(item.Get("type").String()))
			if typ == "" || typ == "text" || typ == "input_text" || typ == "output_text" {
				text := strings.TrimSpace(item.Get("text").String())
				if text != "" {
					parts = append(parts, text)
				}
			}
			return true
		})
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func buildKnowledgeContext(docs []knowledge.Document, searchCfg config.KnowledgeSearchConfig) (string, []string, float64) {
	minScore := searchCfg.MinScore
	if minScore <= 0 {
		minScore = 0.65
	}
	maxChars := searchCfg.MaxContextChars
	if maxChars <= 0 {
		maxChars = 2500
	}

	filtered := make([]knowledge.Document, 0, len(docs))
	scoreSum := 0.0
	for _, doc := range docs {
		score := float64(doc.Score)
		if score < minScore {
			continue
		}
		filtered = append(filtered, doc)
		scoreSum += score
	}
	if len(filtered) == 0 {
		return "", nil, 0
	}
	avgScore := scoreSum / float64(len(filtered))

	byFile := make(map[string]knowledge.Document, len(filtered))
	for _, doc := range filtered {
		filename := extractFilename(doc)
		if existing, ok := byFile[filename]; !ok || doc.Score > existing.Score {
			byFile[filename] = doc
		}
	}

	ordered := make([]knowledge.Document, 0, len(byFile))
	for _, doc := range byFile {
		ordered = append(ordered, doc)
	}
	for i := 0; i < len(ordered)-1; i++ {
		for j := i + 1; j < len(ordered); j++ {
			if ordered[j].Score > ordered[i].Score {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}

	contextParts := make([]string, 0, len(ordered))
	sources := make([]string, 0, len(ordered))
	seenSources := make(map[string]struct{}, len(ordered))
	totalChars := 0

	for _, doc := range ordered {
		if doc.Content == "" {
			continue
		}
		filename := extractFilename(doc)
		entry := fmt.Sprintf("[%s]: %s", filename, doc.Content)
		remaining := maxChars - totalChars
		if remaining <= 0 {
			break
		}
		if len(entry) > remaining {
			if remaining > 100 {
				entry = entry[:remaining] + "..."
			} else {
				break
			}
		}
		contextParts = append(contextParts, entry)
		totalChars += len(entry)
		if _, ok := seenSources[filename]; !ok {
			seenSources[filename] = struct{}{}
			sources = append(sources, filename)
		}
	}

	if len(contextParts) == 0 {
		return "", nil, avgScore
	}
	return strings.Join(contextParts, "\n\n"), sources, avgScore
}

func extractFilename(doc knowledge.Document) string {
	if doc.Metadata != nil {
		if value, ok := doc.Metadata["filename"]; ok {
			if name, ok := value.(string); ok && name != "" {
				return name
			}
		}
	}
	if doc.ID != "" {
		return doc.ID
	}
	return "source"
}

func injectOpenAIRAGContext(rawJSON []byte, message string) []byte {
	messages := gjson.GetBytes(rawJSON, "messages")
	if messages.Exists() && messages.IsArray() {
		msgJSON, _ := json.Marshal(message)
		ragMessage := fmt.Sprintf(`{"role":"system","content":%s}`, msgJSON)

		var builder strings.Builder
		count := 0
		builder.WriteString("[")
		inserted := false
		messages.ForEach(func(_, item gjson.Result) bool {
			if !inserted && item.Get("role").String() != "system" {
				if count > 0 {
					builder.WriteString(",")
				}
				builder.WriteString(ragMessage)
				count++
				inserted = true
			}
			if count > 0 {
				builder.WriteString(",")
			}
			builder.WriteString(item.Raw)
			count++
			return true
		})
		if !inserted {
			if count > 0 {
				builder.WriteString(",")
			}
			builder.WriteString(ragMessage)
		}
		builder.WriteString("]")

		if updated, err := sjson.SetRawBytes(rawJSON, "messages", []byte(builder.String())); err == nil {
			return updated
		}
		return rawJSON
	}

	if instructions := gjson.GetBytes(rawJSON, "instructions"); instructions.Exists() && instructions.Type == gjson.String {
		existing := strings.TrimSpace(instructions.String())
		merged := existing
		if merged == "" {
			merged = message
		} else {
			merged = existing + "\n\n" + message
		}
		if updated, err := sjson.SetBytes(rawJSON, "instructions", merged); err == nil {
			return updated
		}
		return rawJSON
	}

	if gjson.GetBytes(rawJSON, "input").Exists() {
		if updated, err := sjson.SetBytes(rawJSON, "instructions", message); err == nil {
			return updated
		}
	}
	return rawJSON
}

func injectClaudeRAGContext(rawJSON []byte, message string) []byte {
	system := gjson.GetBytes(rawJSON, "system")
	if !system.Exists() {
		if updated, err := sjson.SetBytes(rawJSON, "system", message); err == nil {
			return updated
		}
		return rawJSON
	}

	if system.Type == gjson.String {
		existing := strings.TrimSpace(system.String())
		merged := message
		if existing != "" {
			merged = existing + "\n\n" + message
		}
		if updated, err := sjson.SetBytes(rawJSON, "system", merged); err == nil {
			return updated
		}
		return rawJSON
	}

	if system.IsArray() {
		msgJSON, _ := json.Marshal(message)
		ragMessage := fmt.Sprintf(`{"type":"text","text":%s}`, msgJSON)
		var builder strings.Builder
		count := 0
		builder.WriteString("[")
		system.ForEach(func(_, item gjson.Result) bool {
			if count > 0 {
				builder.WriteString(",")
			}
			builder.WriteString(item.Raw)
			count++
			return true
		})
		if count > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(ragMessage)
		builder.WriteString("]")
		if updated, err := sjson.SetRawBytes(rawJSON, "system", []byte(builder.String())); err == nil {
			return updated
		}
	}
	return rawJSON
}
