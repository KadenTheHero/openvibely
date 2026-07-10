package openaiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
)

const (
	openAIResponsesWebsocketBeta = "responses_websockets=2026-02-06"
	responsesLiteMetadataKey     = "ws_request_header_x_openai_internal_codex_responses_lite"
)

func isResponsesLiteWebsocketModel(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), "gpt-5.6-luna")
}

func (c *Client) responsesWebsocketEndpoint() (string, error) {
	base := strings.TrimSpace(OpenAIChatGPTAPIBaseURL)
	if base == "" {
		return "", fmt.Errorf("missing ChatGPT base URL")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse ChatGPT base URL %q: %w", base, err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("ChatGPT base URL must use http, https, ws, or wss")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/responses"
	return u.String(), nil
}

func buildResponsesLiteWebsocketPayload(payload map[string]any, system, sessionID string) map[string]any {
	request := make(map[string]any, len(payload)+6)
	for key, value := range payload {
		request[key] = value
	}

	input, _ := request["input"].([]any)
	prefix := make([]any, 0, 2)
	tools := request["tools"]
	if tools == nil {
		tools = []any{}
	}
	prefix = append(prefix, map[string]any{
		"type":  "additional_tools",
		"role":  "developer",
		"tools": tools,
	})
	if strings.TrimSpace(system) != "" {
		prefix = append(prefix, map[string]any{
			"type": "message",
			"role": "developer",
			"content": []any{map[string]any{
				"type": "input_text",
				"text": system,
			}},
		})
	}
	request["input"] = append(prefix, input...)
	delete(request, "instructions")
	delete(request, "tools")

	reasoning, _ := request["reasoning"].(map[string]any)
	if reasoning == nil {
		reasoning = map[string]any{"effort": "medium"}
	}
	reasoning["context"] = "all_turns"
	request["reasoning"] = reasoning
	request["type"] = "response.create"
	request["store"] = false
	request["stream"] = true
	request["tool_choice"] = "auto"
	request["parallel_tool_calls"] = false
	request["include"] = []string{"reasoning.encrypted_content"}
	request["client_metadata"] = map[string]string{
		responsesLiteMetadataKey: "true",
		"session_id":             sessionID,
	}
	return request
}

func (c *Client) openResponsesWebsocketStream(ctx context.Context, payload map[string]any) (io.ReadCloser, error) {
	endpoint, err := c.responsesWebsocketEndpoint()
	if err != nil {
		return nil, err
	}

	dial := func() (*websocket.Conn, *http.Response, error) {
		headers := http.Header{}
		req, reqErr := http.NewRequest(http.MethodGet, endpoint, nil)
		if reqErr != nil {
			return nil, nil, reqErr
		}
		c.applyAuthHeaders(req, true)
		for key, values := range req.Header {
			headers[key] = append([]string(nil), values...)
		}
		headers.Set("OpenAI-Beta", openAIResponsesWebsocketBeta)
		headers.Set("session-id", c.sessionID)
		headers.Set("thread-id", c.sessionID)
		return websocket.Dial(ctx, endpoint, &websocket.DialOptions{
			HTTPClient:      c.httpClient,
			HTTPHeader:      headers,
			CompressionMode: websocket.CompressionContextTakeover,
		})
	}

	tokenUsed := c.auth.Token
	conn, resp, err := dial()
	if err != nil && resp != nil && resp.StatusCode == http.StatusUnauthorized && c.oauthUnauthorizedHandler != nil {
		if resp.Body != nil {
			resp.Body.Close()
		}
		tokens, recovered, recoverErr := c.oauthUnauthorizedHandler(ctx, tokenUsed)
		if recoverErr != nil {
			return nil, recoverErr
		}
		if recovered {
			c.applyOAuthTokens(tokens)
			conn, resp, err = dial()
		}
	}
	if err != nil {
		if resp != nil && resp.Body != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			return nil, fmt.Errorf("connect Responses websocket %q: %d %s %s", endpoint, resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(body)))
		}
		return nil, fmt.Errorf("connect Responses websocket %q: %w", endpoint, err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "marshal request")
		return nil, fmt.Errorf("marshal websocket request: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, body); err != nil {
		conn.Close(websocket.StatusInternalError, "send request")
		return nil, fmt.Errorf("send Responses websocket request: %w", err)
	}

	reader, writer := io.Pipe()
	go func() {
		defer conn.Close(websocket.StatusNormalClosure, "")
		defer writer.Close()
		for {
			messageType, data, readErr := conn.Read(ctx)
			if readErr != nil {
				writer.CloseWithError(fmt.Errorf("read Responses websocket: %w", readErr))
				return
			}
			if messageType != websocket.MessageText {
				writer.CloseWithError(fmt.Errorf("read Responses websocket: unexpected binary frame"))
				return
			}
			if _, writeErr := fmt.Fprintf(writer, "data: %s\n\n", data); writeErr != nil {
				return
			}
			var event struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(data, &event) == nil && isTerminalResponsesWebsocketEvent(event.Type) {
				return
			}
		}
	}()
	return reader, nil
}

func isTerminalResponsesWebsocketEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "response.completed", "response.failed", "response.incomplete", "response.error", "error":
		return true
	default:
		return false
	}
}
