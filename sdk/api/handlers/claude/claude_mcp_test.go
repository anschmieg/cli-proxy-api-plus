package claude

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	"github.com/tidwall/gjson"
)

func TestRunClaudeMCPToolLoopExecutesTools(t *testing.T) {
	request := []byte(`{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	resp1 := []byte(`{"id":"msg_1","model":"claude-3-5-sonnet","content":[{"type":"tool_use","id":"toolu_1","name":"web_search","input":{"query":"hi"}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`)
	resp2 := []byte(`{"id":"msg_2","model":"claude-3-5-sonnet","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":2}}`)

	var payloads [][]byte
	execute := func(ctx context.Context, rawJSON []byte) ([]byte, *interfaces.ErrorMessage) {
		payloads = append(payloads, rawJSON)
		if len(payloads) == 1 {
			return resp1, nil
		}
		return resp2, nil
	}

	execTool := func(ctx context.Context, name string, args map[string]any) (any, error) {
		return map[string]any{"ok": true}, nil
	}

	state := &handlers.MCPToolState{
		ToolNames: map[string]struct{}{"web_search": {}},
	}

	finalResp, errMsg := runClaudeMCPToolLoop(context.Background(), request, state, execute, execTool)
	if errMsg != nil {
		t.Fatalf("unexpected error: %v", errMsg)
	}
	if string(finalResp) != string(resp2) {
		t.Fatalf("expected final response to be resp2")
	}
	if len(payloads) != 2 {
		t.Fatalf("expected 2 model calls, got %d", len(payloads))
	}
	if got := gjson.GetBytes(payloads[1], "messages.#").Int(); got < 3 {
		t.Fatalf("expected tool results to be appended to messages, got %d messages", got)
	}
	if role := gjson.GetBytes(payloads[1], "messages.2.role").String(); role != "user" {
		t.Fatalf("expected third message role to be user (tool_result), got %q", role)
	}
}
