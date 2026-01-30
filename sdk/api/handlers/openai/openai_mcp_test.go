package openai

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	"github.com/tidwall/gjson"
)

func TestRunOpenAIMCPToolLoopExecutesTools(t *testing.T) {
	request := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	resp1 := []byte(`{"id":"chatcmpl_1","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{\\"q\\":\\"hi\\"}"}}]}}]}`)
	resp2 := []byte(`{"id":"chatcmpl_2","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)

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

	finalResp, errMsg := runOpenAIMCPToolLoop(context.Background(), request, map[string]struct{}{"search": {}}, execute, execTool)
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
	if role := gjson.GetBytes(payloads[1], "messages.2.role").String(); role != "tool" {
		t.Fatalf("expected third message role to be tool, got %q", role)
	}
}
