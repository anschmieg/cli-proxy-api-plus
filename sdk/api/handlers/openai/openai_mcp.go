package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/constant"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	codexconverter "github.com/router-for-me/CLIProxyAPI/v6/internal/translator/codex/openai/chat-completions"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const maxMCPLoops = 10

type openAIToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type openAIExecuteFunc func(ctx context.Context, rawJSON []byte) ([]byte, *interfaces.ErrorMessage)
type openAIToolExecFunc func(ctx context.Context, name string, args map[string]any) (any, error)

func (h *OpenAIAPIHandler) handleChatCompletionsWithMCP(
	c *gin.Context,
	rawJSON []byte,
	mcpState *handlers.MCPToolState,
	useResponses bool,
	originalStream bool,
	alt string,
) {
	if mcpState == nil || len(mcpState.ToolNames) == 0 {
		return
	}

	execute := func(ctx context.Context, payload []byte) ([]byte, *interfaces.ErrorMessage) {
		modelName := gjson.GetBytes(payload, "model").String()
		if !useResponses {
			cliCtx, cliCancel := h.GetContextWithCancel(h, c, ctx)
			resp, errMsg := h.ExecuteWithAuthManager(cliCtx, h.HandlerType(), modelName, payload, alt)
			if errMsg != nil {
				cliCancel(errMsg.Error)
			} else {
				cliCancel(nil)
			}
			return resp, errMsg
		}

		responsesJSON := codexconverter.ConvertOpenAIRequestToCodex(modelName, payload, false)
		cliCtx, cliCancel := h.GetContextWithCancel(h, c, ctx)
		resp, errMsg := h.ExecuteWithAuthManager(cliCtx, constant.OpenaiResponse, modelName, responsesJSON, alt)
		if errMsg != nil {
			cliCancel(errMsg.Error)
			return nil, errMsg
		}
		converted := convertResponsesObjectToChatCompletion(cliCtx, modelName, payload, responsesJSON, resp)
		if converted == nil {
			cliCancel(fmt.Errorf("response conversion failed"))
			return nil, &interfaces.ErrorMessage{
				StatusCode: http.StatusInternalServerError,
				Error:      fmt.Errorf("failed to convert response to chat completion format"),
			}
		}
		cliCancel(nil)
		return converted, nil
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

	finalResp, errMsg := runOpenAIMCPToolLoop(c.Request.Context(), rawJSON, mcpState.ToolNames, execute, execTool)
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
		h.streamOpenAIResponseFromBuffer(c, finalResp)
		return
	}

	c.Header("Content-Type", "application/json")
	_, _ = c.Writer.Write(finalResp)
}

func runOpenAIMCPToolLoop(
	ctx context.Context,
	rawJSON []byte,
	mcpToolNames map[string]struct{},
	execute openAIExecuteFunc,
	execTool openAIToolExecFunc,
) ([]byte, *interfaces.ErrorMessage) {
	if len(mcpToolNames) == 0 {
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
		finalResp = resp

		toolCalls := extractOpenAIToolCalls(resp)
		if len(toolCalls) == 0 {
			break
		}

		var mcpCalls []openAIToolCall
		var clientCalls []openAIToolCall
		for _, call := range toolCalls {
			if _, ok := mcpToolNames[call.Name]; ok {
				mcpCalls = append(mcpCalls, call)
			} else {
				clientCalls = append(clientCalls, call)
			}
		}

		if len(mcpCalls) == 0 {
			if len(clientCalls) > 0 {
				break
			}
			break
		}

		assistantMessage := buildOpenAIAssistantMessage(resp)
		nextPayload, err := appendOpenAIMessage(payload, assistantMessage)
		if err != nil {
			return nil, &interfaces.ErrorMessage{
				StatusCode: http.StatusInternalServerError,
				Error:      err,
			}
		}
		payload = nextPayload

		for _, call := range mcpCalls {
			args := map[string]any{}
			if call.Arguments != "" {
				if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
					args = map[string]any{}
				}
			}

			result, err := execTool(ctx, call.Name, args)
			var toolContent []byte
			if err != nil {
				toolContent, _ = json.Marshal(map[string]any{"error": err.Error()})
			} else {
				if result == nil {
					toolContent = []byte(`{}`)
				} else if raw, marshalErr := json.Marshal(result); marshalErr == nil {
					toolContent = raw
				} else {
					toolContent, _ = json.Marshal(map[string]any{"error": marshalErr.Error()})
				}
			}
			message := buildOpenAIToolResultMessage(call.ID, string(toolContent))
			payload, err = appendOpenAIMessage(payload, message)
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

func extractOpenAIToolCalls(resp []byte) []openAIToolCall {
	var calls []openAIToolCall
	toolCalls := gjson.GetBytes(resp, "choices.0.message.tool_calls")
	if !toolCalls.Exists() || !toolCalls.IsArray() {
		return nil
	}
	toolCalls.ForEach(func(_, item gjson.Result) bool {
		name := strings.TrimSpace(item.Get("function.name").String())
		if name == "" {
			return true
		}
		calls = append(calls, openAIToolCall{
			ID:        item.Get("id").String(),
			Name:      name,
			Arguments: item.Get("function.arguments").String(),
		})
		return true
	})
	return calls
}

func buildOpenAIAssistantMessage(resp []byte) string {
	message := gjson.GetBytes(resp, "choices.0.message")
	raw := message.Raw
	if raw == "" {
		raw = `{"role":"assistant","content":""}`
	} else if !message.Get("role").Exists() {
		updated, err := sjson.Set(raw, "role", "assistant")
		if err == nil {
			raw = updated
		}
	}
	return raw
}

func buildOpenAIToolResultMessage(callID string, content string) string {
	msg := `{"role":"tool","tool_call_id":"","content":""}`
	msg, _ = sjson.Set(msg, "tool_call_id", callID)
	msg, _ = sjson.Set(msg, "content", content)
	return msg
}

func appendOpenAIMessage(rawJSON []byte, messageJSON string) ([]byte, error) {
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

func (h *OpenAIAPIHandler) streamOpenAIResponseFromBuffer(c *gin.Context, resp []byte) {
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

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	chunk := buildOpenAIChunkFromResponse(resp)
	if chunk != nil {
		_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", chunk)
		flusher.Flush()
	}
	_, _ = fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	flusher.Flush()
}

func buildOpenAIChunkFromResponse(resp []byte) []byte {
	if !gjson.ValidBytes(resp) {
		return nil
	}
	root := gjson.ParseBytes(resp)
	id := root.Get("id").String()
	model := root.Get("model").String()
	created := root.Get("created").Int()

	message := root.Get("choices.0.message")
	role := message.Get("role").String()
	content := message.Get("content")
	toolCalls := message.Get("tool_calls")
	finishReason := root.Get("choices.0.finish_reason").String()
	if finishReason == "" {
		finishReason = root.Get("choices.0.finish_reason").String()
	}

	chunk := `{"id":"","object":"chat.completion.chunk","created":0,"model":"","choices":[{"index":0,"delta":{},"finish_reason":""}]}`
	chunk, _ = sjson.Set(chunk, "id", id)
	chunk, _ = sjson.Set(chunk, "object", "chat.completion.chunk")
	if created > 0 {
		chunk, _ = sjson.Set(chunk, "created", created)
	}
	if model != "" {
		chunk, _ = sjson.Set(chunk, "model", model)
	}
	if role != "" {
		chunk, _ = sjson.Set(chunk, "choices.0.delta.role", role)
	} else {
		chunk, _ = sjson.Set(chunk, "choices.0.delta.role", "assistant")
	}
	if content.Exists() {
		if content.Type == gjson.String {
			chunk, _ = sjson.Set(chunk, "choices.0.delta.content", content.String())
		} else {
			chunk, _ = sjson.SetRaw(chunk, "choices.0.delta.content", content.Raw)
		}
	}
	if toolCalls.Exists() {
		chunk, _ = sjson.SetRaw(chunk, "choices.0.delta.tool_calls", util.FixJSON(toolCalls.Raw))
	}
	if finishReason == "" {
		finishReason = "stop"
	}
	chunk, _ = sjson.Set(chunk, "choices.0.finish_reason", finishReason)
	return []byte(chunk)
}
