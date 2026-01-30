package handlers

import (
	"context"
	"strings"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/knowledge"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	"github.com/tidwall/gjson"
)

func TestResolveProfileFromModel_Suffix(t *testing.T) {
	profiles := []internalconfig.Profile{
		{ID: "rag", DefaultModel: "gpt-4o"},
	}

	profile, upstream, errMsg := resolveProfileFromModel("gpt-4o@rag", profiles)
	if errMsg != nil {
		t.Fatalf("unexpected error: %v", errMsg)
	}
	if profile == nil || profile.ID != "rag" {
		t.Fatalf("expected profile rag, got %#v", profile)
	}
	if upstream != "gpt-4o" {
		t.Fatalf("expected upstream gpt-4o, got %q", upstream)
	}
}

func TestResolveProfileFromModel_DefaultModel(t *testing.T) {
	profiles := []internalconfig.Profile{
		{ID: "assistant", DefaultModel: "gpt-4o-mini"},
	}

	profile, upstream, errMsg := resolveProfileFromModel("assistant", profiles)
	if errMsg != nil {
		t.Fatalf("unexpected error: %v", errMsg)
	}
	if profile == nil || profile.ID != "assistant" {
		t.Fatalf("expected profile assistant, got %#v", profile)
	}
	if upstream != "gpt-4o-mini" {
		t.Fatalf("expected upstream gpt-4o-mini, got %q", upstream)
	}
}

func TestResolveProfileFromModel_MissingDefaultModel(t *testing.T) {
	profiles := []internalconfig.Profile{
		{ID: "assistant"},
	}

	_, _, errMsg := resolveProfileFromModel("assistant", profiles)
	if errMsg == nil || errMsg.StatusCode == 0 {
		t.Fatalf("expected error for missing default model")
	}
}

func TestApplyOpenAISystemPrompt_Messages(t *testing.T) {
	raw := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`)
	updated := applyOpenAISystemPrompt(raw, "Be helpful.")

	if role := gjson.GetBytes(updated, "messages.0.role").String(); role != "system" {
		t.Fatalf("expected system role, got %q", role)
	}
	if content := gjson.GetBytes(updated, "messages.0.content").String(); content != "Be helpful." {
		t.Fatalf("expected system content, got %q", content)
	}
}

func TestApplyOpenAISystemPrompt_Instructions(t *testing.T) {
	raw := []byte(`{"model":"gpt-4o","instructions":"Existing."}`)
	updated := applyOpenAISystemPrompt(raw, "Prefix.")

	value := gjson.GetBytes(updated, "instructions").String()
	if value != "Prefix.\n\nExisting." {
		t.Fatalf("expected merged instructions, got %q", value)
	}
}

func TestApplyOpenAISystemPrompt_InputOnly(t *testing.T) {
	raw := []byte(`{"model":"gpt-4o","input":[{"role":"user","content":"hi"}]}`)
	updated := applyOpenAISystemPrompt(raw, "Prefix.")

	value := gjson.GetBytes(updated, "instructions").String()
	if value != "Prefix." {
		t.Fatalf("expected instructions set, got %q", value)
	}
}

func TestApplyClaudeSystemPrompt_String(t *testing.T) {
	raw := []byte(`{"model":"claude-3-5-sonnet","system":"Existing.","messages":[]}`)
	updated := applyClaudeSystemPrompt(raw, "Prefix.")

	value := gjson.GetBytes(updated, "system").String()
	if value != "Prefix.\n\nExisting." {
		t.Fatalf("expected merged system prompt, got %q", value)
	}
}

func TestApplyClaudeSystemPrompt_Array(t *testing.T) {
	raw := []byte(`{"model":"claude-3-5-sonnet","system":[{"type":"text","text":"Existing."}],"messages":[]}`)
	updated := applyClaudeSystemPrompt(raw, "Prefix.")

	first := gjson.GetBytes(updated, "system.0.text").String()
	if first != "Prefix." {
		t.Fatalf("expected first system item, got %q", first)
	}
}

func TestApplyProfileTools_Disabled(t *testing.T) {
	disabled := false
	tools := internalconfig.ProfileTools{Enabled: &disabled}
	raw := []byte(`{"tools":[{"type":"function","function":{"name":"one"}}],"tool_choice":"auto","functions":[{"name":"two"}],"function_call":"auto"}`)
	updated := applyProfileTools(raw, tools)

	if gjson.GetBytes(updated, "tools").Exists() || gjson.GetBytes(updated, "tool_choice").Exists() {
		t.Fatalf("expected tools removed")
	}
	if gjson.GetBytes(updated, "functions").Exists() || gjson.GetBytes(updated, "function_call").Exists() {
		t.Fatalf("expected functions removed")
	}
}

func TestApplyProfileTools_Allowlist(t *testing.T) {
	tools := internalconfig.ProfileTools{Allowlist: []string{"allowed"}}
	raw := []byte(`{"tools":[{"type":"function","function":{"name":"allowed"}},{"type":"function","function":{"name":"blocked"}}],"tool_choice":{"type":"function","function":{"name":"blocked"}}}`)
	updated := applyProfileTools(raw, tools)

	if size := gjson.GetBytes(updated, "tools.#").Int(); size != 1 {
		t.Fatalf("expected 1 tool, got %d", size)
	}
	if gjson.GetBytes(updated, "tools.0.function.name").String() != "allowed" {
		t.Fatalf("expected allowed tool")
	}
	if gjson.GetBytes(updated, "tool_choice").Exists() {
		t.Fatalf("expected tool_choice removed")
	}
}

func TestApplyProfileTools_FunctionsAllowlist(t *testing.T) {
	tools := internalconfig.ProfileTools{Allowlist: []string{"fn-1"}}
	raw := []byte(`{"functions":[{"name":"fn-1"},{"name":"fn-2"}],"function_call":"auto"}`)
	updated := applyProfileTools(raw, tools)

	if size := gjson.GetBytes(updated, "functions.#").Int(); size != 1 {
		t.Fatalf("expected 1 function, got %d", size)
	}
	if gjson.GetBytes(updated, "functions.0.name").String() != "fn-1" {
		t.Fatalf("expected fn-1 to remain")
	}
	if gjson.GetBytes(updated, "function_call").String() != "auto" {
		t.Fatalf("expected function_call retained")
	}
}

type staticEmbedder struct {
	vector []float32
}

func (e *staticEmbedder) GetModel() string {
	return "static"
}

func (e *staticEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = e.vector
	}
	return out, nil
}

func TestApplyProfileToPayload_KnowledgeBase(t *testing.T) {
	store := knowledge.NewMemoryStore()
	embedder := &staticEmbedder{vector: []float32{1, 0}}
	manager := knowledge.NewManager(store, embedder)

	err := store.Add(context.Background(), []knowledge.Document{
		{
			ID:      "doc-1",
			Content: "Reference content",
			Vector:  []float32{1, 0},
			Metadata: map[string]interface{}{
				"project":  "project-1",
				"filename": "file.txt",
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to seed store: %v", err)
	}

	cfg := &config.Config{
		Knowledge: config.KnowledgeConfig{
			Enabled: true,
			Search: config.KnowledgeSearchConfig{
				Limit:           5,
				MinScore:        0.1,
				MaxContextChars: 500,
			},
		},
		Profiles: []config.Profile{
			{
				ID:            "rag",
				DefaultModel:  "gpt-4o",
				KnowledgeBase: "project-1",
			},
		},
	}

	handler := NewBaseAPIHandlersWithConfig(cfg, nil)
	handler.UpdateKnowledgeManager(manager)

	raw := []byte(`{"model":"gpt-4o@rag","messages":[{"role":"user","content":"hello"}]}`)
	updated, errMsg := handler.ApplyProfileToPayload(context.Background(), "openai", raw)
	if errMsg != nil {
		t.Fatalf("unexpected error: %v", errMsg)
	}

	if model := gjson.GetBytes(updated, "model").String(); model != "gpt-4o" {
		t.Fatalf("expected upstream model, got %q", model)
	}

	content := gjson.GetBytes(updated, "messages.0.content").String()
	if content == "" || !strings.Contains(content, "knowledge_base") {
		t.Fatalf("expected knowledge base content in system message")
	}
}
