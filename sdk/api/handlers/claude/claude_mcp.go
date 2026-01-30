package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type claudeToolUse struct {
	ID    string
	Name  string
	Input gjson.Result
}

const maxMCPLoops = 10

type claudeExecuteFunc func(ctx context.Context, rawJSON []byte) ([]byte, *interfaces.ErrorMessage)
type claudeToolExecFunc func(ctx context.Context, name string, args map[string]any) (any, error)

func (h *ClaudeCodeAPIHandler) handleMessagesWithMCP(
	c *gin.Context,
	rawJSON []byte,
	mcpState *handlers.MCPToolState,
	originalStream bool,
	alt string,
) {
	if mcpState == nil || len(mcpState.ToolNames) == 0 {
		return
	}

	execute := func(ctx context.Context, payload []byte) ([]byte, *interfaces.ErrorMessage) {
		modelName := gjson.GetBytes(payload, "model").String()
		cliCtx, cliCancel := h.GetContextWithCancel(h, c, ctx)
		resp, errMsg := h.ExecuteWithAuthManager(cliCtx, h.HandlerType(), modelName, payload, alt)
		if errMsg != nil {
			cliCancel(errMsg.Error)
		} else {
			cliCancel(nil)
		}
		return resp, errMsg
	}

	execTool := func(ctx context.Context, name string, args map[string]any) (any, error) {
		if h.MCPService == nil {
			return nil, fmt.Errorf("mcp service not configured")
		}
		result, err := h.MCPService.ExecuteTool(ctx, name, args)
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	finalResp, errMsg := runClaudeMCPToolLoop(c.Request.Context(), rawJSON, mcpState, execute, execTool)
	if errMsg != nil {
		h.WriteErrorResponse(c, errMsg)
		return
	}

	if finalResp == nil {
		h.WriteErrorResponse(c, &interfaces.ErrorMessage{
			StatusCode: http.StatusInternalServerError,
			Error:      fmt.Errorf("no response from upstream"),
		})
		return
	}

	if originalStream {
		h.streamClaudeResponseFromBuffer(c, finalResp)
		return
	}

	c.Header("Content-Type", "application/json")
	_, _ = c.Writer.Write(finalResp)
}

func runClaudeMCPToolLoop(
	ctx context.Context,
	rawJSON []byte,
	mcpState *handlers.MCPToolState,
	execute claudeExecuteFunc,
	execTool claudeToolExecFunc,
) ([]byte, *interfaces.ErrorMessage) {
	if mcpState == nil || len(mcpState.ToolNames) == 0 {
		return nil, nil
	}

	payload := rawJSON
	if updated, err := sjson.SetBytes(payload, "stream", false); err == nil {
		payload = updated
	}

	var finalResp []byte
	for loop := 0; loop < maxMCPLoops; loop++ {
		resp, errMsg := execute(ctx, payload)
		if errMsg != nil {
			return nil, errMsg
		}
		resp = maybeDecompressClaudeResponse(resp)
		finalResp = resp

		toolUses := extractClaudeToolUses(resp)
		if len(toolUses) == 0 {
			break
		}

		var mcpCalls []claudeToolUse
		for _, call := range toolUses {
			if _, ok := mcpState.ToolNames[call.Name]; ok {
				mcpCalls = append(mcpCalls, call)
			}
		}
		if len(mcpCalls) == 0 {
			break
		}

		assistantMessage := buildClaudeAssistantMessage(resp)
		nextPayload, err := appendClaudeMessage(payload, assistantMessage)
		if err != nil {
			return nil, &interfaces.ErrorMessage{
				StatusCode: http.StatusInternalServerError,
				Error:      err,
			}
		}
		payload = nextPayload

		for _, call := range mcpCalls {
			args := map[string]any{}
			if call.Input.Exists() && call.Input.Raw != "" {
				if err := json.Unmarshal([]byte(call.Input.Raw), &args); err != nil {
					args = map[string]any{}
				}
			}
			result, err := execTool(ctx, call.Name, args)
			isError := false
			var toolContent []byte
			if err != nil {
				isError = true
				toolContent, _ = json.Marshal(map[string]any{"error": err.Error()})
			} else {
				if result == nil {
					toolContent = []byte(`{}`)
				} else if raw, marshalErr := json.Marshal(result); marshalErr == nil {
					toolContent = raw
				} else {
					isError = true
					toolContent, _ = json.Marshal(map[string]any{"error": marshalErr.Error()})
				}
			}

			message := buildClaudeToolResultMessage(call.ID, string(toolContent), isError)
			payload, err = appendClaudeMessage(payload, message)
			if err != nil {
				return nil, &interfaces.ErrorMessage{
					StatusCode: http.StatusInternalServerError,
					Error:      err,
				}
			}
		}
	}

	return finalResp, nil
}

func extractClaudeToolUses(resp []byte) []claudeToolUse {
	content := gjson.GetBytes(resp, "content")
	if !content.Exists() || !content.IsArray() {
		return nil
	}
	var toolUses []claudeToolUse
	content.ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() != "tool_use" {
			return true
		}
		name := strings.TrimSpace(item.Get("name").String())
		if name == "" {
			return true
		}
		toolUses = append(toolUses, claudeToolUse{
			ID:    item.Get("id").String(),
			Name:  name,
			Input: item.Get("input"),
		})
		return true
	})
	return toolUses
}

func buildClaudeAssistantMessage(resp []byte) string {
	message := `{"role":"assistant","content":[]}`
	content := gjson.GetBytes(resp, "content")
	if content.Exists() && content.IsArray() {
		message, _ = sjson.SetRaw(message, "content", content.Raw)
	}
	return message
}

func buildClaudeToolResultMessage(toolUseID string, content string, isError bool) string {
	block := `{"type":"tool_result","tool_use_id":"","content":""}`
	block, _ = sjson.Set(block, "tool_use_id", toolUseID)
	block, _ = sjson.Set(block, "content", content)
	if isError {
		block, _ = sjson.Set(block, "is_error", true)
	}
	message := `{"role":"user","content":[]}`
	message, _ = sjson.SetRaw(message, "content", fmt.Sprintf("[%s]", block))
	return message
}

func appendClaudeMessage(rawJSON []byte, messageJSON string) ([]byte, error) {
	updated := rawJSON
	if !gjson.GetBytes(updated, "messages").Exists() {
		next, err := sjson.SetRawBytes(updated, "messages", []byte("[]"))
		if err != nil {
			return rawJSON, err
		}
		updated = next
	}
	next, err := sjson.SetRawBytes(updated, "messages.-1", []byte(messageJSON))
	if err != nil {
		return rawJSON, err
	}
	return next, nil
}

func (h *ClaudeCodeAPIHandler) streamClaudeResponseFromBuffer(c *gin.Context, resp []byte) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: "Streaming not supported",
				Type:    "server_error",
			},
		})
		return
	}

	root := gjson.ParseBytes(resp)
	id := root.Get("id").String()
	model := root.Get("model").String()
	stopReason := root.Get("stop_reason").String()
	stopSequence := root.Get("stop_sequence").String()
	usage := root.Get("usage")

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	messageStart := `{"type":"message_start","message":{"id":"","type":"message","role":"assistant","model":"","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`
	messageStart, _ = sjson.Set(messageStart, "message.id", id)
	messageStart, _ = sjson.Set(messageStart, "message.model", model)
	if usage.Exists() {
		if v := usage.Get("input_tokens"); v.Exists() {
			messageStart, _ = sjson.Set(messageStart, "message.usage.input_tokens", v.Int())
		}
	}
	_, _ = fmt.Fprintf(c.Writer, "event: message_start\ndata: %s\n\n", messageStart)
	flusher.Flush()

	content := root.Get("content")
	if content.Exists() && content.IsArray() {
		content.ForEach(func(index, item gjson.Result) bool {
			blockType := item.Get("type").String()
			blockIndex := int(index.Int())
			switch blockType {
			case "text":
				start := `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`
				start, _ = sjson.Set(start, "index", blockIndex)
				_, _ = fmt.Fprintf(c.Writer, "event: content_block_start\ndata: %s\n\n", start)

				delta := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`
				delta, _ = sjson.Set(delta, "index", blockIndex)
				delta, _ = sjson.Set(delta, "delta.text", item.Get("text").String())
				_, _ = fmt.Fprintf(c.Writer, "event: content_block_delta\ndata: %s\n\n", delta)

				stop := `{"type":"content_block_stop","index":0}`
				stop, _ = sjson.Set(stop, "index", blockIndex)
				_, _ = fmt.Fprintf(c.Writer, "event: content_block_stop\ndata: %s\n\n", stop)
			case "thinking":
				start := `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`
				start, _ = sjson.Set(start, "index", blockIndex)
				_, _ = fmt.Fprintf(c.Writer, "event: content_block_start\ndata: %s\n\n", start)

				delta := `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}`
				delta, _ = sjson.Set(delta, "index", blockIndex)
				delta, _ = sjson.Set(delta, "delta.thinking", item.Get("thinking").String())
				_, _ = fmt.Fprintf(c.Writer, "event: content_block_delta\ndata: %s\n\n", delta)

				stop := `{"type":"content_block_stop","index":0}`
				stop, _ = sjson.Set(stop, "index", blockIndex)
				_, _ = fmt.Fprintf(c.Writer, "event: content_block_stop\ndata: %s\n\n", stop)
			case "tool_use":
				start := `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"","name":"","input":{}}}`
				start, _ = sjson.Set(start, "index", blockIndex)
				start, _ = sjson.Set(start, "content_block.id", item.Get("id").String())
				start, _ = sjson.Set(start, "content_block.name", item.Get("name").String())
				_, _ = fmt.Fprintf(c.Writer, "event: content_block_start\ndata: %s\n\n", start)

				delta := `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`
				delta, _ = sjson.Set(delta, "index", blockIndex)
				delta, _ = sjson.Set(delta, "delta.partial_json", util.FixJSON(item.Get("input").Raw))
				_, _ = fmt.Fprintf(c.Writer, "event: content_block_delta\ndata: %s\n\n", delta)

				stop := `{"type":"content_block_stop","index":0}`
				stop, _ = sjson.Set(stop, "index", blockIndex)
				_, _ = fmt.Fprintf(c.Writer, "event: content_block_stop\ndata: %s\n\n", stop)
			}
			flusher.Flush()
			return true
		})
	}

	messageDelta := `{"type":"message_delta","delta":{"stop_reason":"","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`
	messageDelta, _ = sjson.Set(messageDelta, "delta.stop_reason", stopReason)
	if stopSequence != "" {
		messageDelta, _ = sjson.Set(messageDelta, "delta.stop_sequence", stopSequence)
	}
	if usage.Exists() {
		if v := usage.Get("input_tokens"); v.Exists() {
			messageDelta, _ = sjson.Set(messageDelta, "usage.input_tokens", v.Int())
		}
		if v := usage.Get("output_tokens"); v.Exists() {
			messageDelta, _ = sjson.Set(messageDelta, "usage.output_tokens", v.Int())
		}
	}
	_, _ = fmt.Fprintf(c.Writer, "event: message_delta\ndata: %s\n\n", messageDelta)
	_, _ = fmt.Fprintf(c.Writer, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	flusher.Flush()
}
