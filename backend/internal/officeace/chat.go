package officeace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"codearts2api/internal/provider"
	"codearts2api/internal/providerhttp"
)

// Models with known quirks that do not accept thinking/reasoning parameters.
func quirksFor(model string) bool {
	m := strings.ToLower(model)
	return strings.HasPrefix(m, "glm-5.3") || strings.HasPrefix(m, "glm-5-test")
}

func (c *Client) Stream(ctx context.Context, raw json.RawMessage, r provider.Request, emit func([]byte) error) error {
	norm, _, err := c.Normalize(raw)
	if err != nil {
		return err
	}
	if !providerhttp.ModelID(r.Model) {
		return invalid("型号无效")
	}

	var cred Credentials
	if err := json.Unmarshal(norm, &cred); err != nil {
		return err
	}

	// Heal MaaS credentials if missing
	if cred.ModelAppKey == "" || cred.ModelAppSecret == "" {
		if info, err := c.fetchModelInfo(ctx, cred); err == nil && info.AppKey != "" {
			cred.ModelAppKey = info.AppKey
			cred.ModelAppSecret = info.AppSecret
			if info.BaseURL != "" {
				cred.MaaSBaseURL = info.BaseURL
			}
		}
	}
	if cred.ModelAppKey == "" || cred.ModelAppSecret == "" {
		return &provider.Error{
			Status:  403,
			Code:    "OFFICEACE_NOT_PROVISIONED",
			Message: UnsubscribedReason,
		}
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(r.Body, &body); err != nil || body == nil {
		return invalid("请求须为 JSON 对象")
	}

	var messages []json.RawMessage
	if err := json.Unmarshal(body["messages"], &messages); err != nil || len(messages) == 0 {
		return invalid("缺少 messages")
	}

	body["model"], _ = json.Marshal(r.Model)
	body["stream"] = json.RawMessage("true")

	// Strip thinking parameters for GLM-5.3/GLM-5 models that fail on them
	if quirksFor(r.Model) {
		delete(body, "thinking")
		delete(body, "enable_thinking")
		delete(body, "reasoning_effort")
		delete(body, "stream_options")
	} else {
		// Include usage in stream_options
		options := make(map[string]json.RawMessage)
		if b, ok := body["stream_options"]; ok && len(b) > 0 && string(b) != "null" {
			_ = json.Unmarshal(b, &options)
		}
		options["include_usage"] = json.RawMessage("true")
		body["stream_options"], _ = json.Marshal(options)
	}

	delete(body, "user_report")

	reqBytes, err := json.Marshal(body)
	if err != nil {
		return invalid("请求序列化失败")
	}

	base, err := c.maasBase(cred)
	if err != nil {
		return err
	}
	chatURL := base + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(reqBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", basicAuthHeader(cred.ModelAppKey, cred.ModelAppSecret))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "codearts2api-panel/officeace")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return provider.NewTransportError("OfficeAce", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return c.classifyError(resp)
	}

	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType != "text/event-stream" {
		return providerhttp.Broken("OfficeAce")
	}

	seen := false
	finished := false

	return providerhttp.SSE(ctx, resp.Body, "OfficeAce", func(event string, b []byte) error {
		trimmed := strings.TrimSpace(string(b))
		if trimmed == "[DONE]" {
			if !seen || !finished {
				return providerhttp.Broken("OfficeAce")
			}
			return providerhttp.End
		}

		var chunk map[string]json.RawMessage
		if err := json.Unmarshal(b, &chunk); err != nil || chunk == nil {
			return providerhttp.Broken("OfficeAce")
		}

		// Upstream error embedded in chunk
		if errRaw, ok := chunk["error"]; ok && len(errRaw) > 0 && string(errRaw) != "null" {
			return parseChunkError(errRaw)
		}

		var choices []struct {
			Index  int                        `json:"index"`
			Delta  map[string]json.RawMessage `json:"delta"`
			Finish *string                    `json:"finish_reason"`
		}
		if err := json.Unmarshal(chunk["choices"], &choices); err != nil {
			return providerhttp.Broken("OfficeAce")
		}

		if len(choices) == 0 {
			// Usage chunk
			if !seen || len(chunk["usage"]) == 0 || string(chunk["usage"]) == "null" {
				return providerhttp.Broken("OfficeAce")
			}
		} else {
			if len(choices) != 1 || choices[0].Index != 0 {
				return providerhttp.Broken("OfficeAce")
			}
			ch := choices[0]
			if finished || ch.Delta == nil && (ch.Finish == nil || *ch.Finish == "") {
				return providerhttp.Broken("OfficeAce")
			}
			seen = true
			if ch.Finish != nil && *ch.Finish != "" {
				finished = true
			}
		}

		return emit(b)
	})
}

func (c *Client) classifyError(resp *http.Response) error {
	status := resp.StatusCode
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))

	var obj map[string]json.RawMessage
	_ = json.Unmarshal(bodyBytes, &obj)

	code := firstString(obj, "error_code", "errorCode", "code")
	msg := firstString(obj, "error_msg", "errorMsg", "message")
	if subObj, ok := obj["error"]; ok && len(subObj) > 0 {
		var errData map[string]json.RawMessage
		if json.Unmarshal(subObj, &errData) == nil {
			if c := firstString(errData, "code", "error_code"); c != "" {
				code = c
			}
			if m := firstString(errData, "message", "error_msg"); m != "" {
				msg = m
			}
		}
	}

	fault := false
	blocked := false

	switch {
	case strings.Contains(code, "APIG.1001") || strings.Contains(code, "APIG.1002") || strings.Contains(code, "APIG.1009") || status == 401:
		fault = true
	case strings.Contains(code, "ModelArts.81111") || strings.Contains(code, "ModelArts.81112") || strings.Contains(code, "ModelArts.81114") || strings.Contains(code, "0308"):
		status = 429
	case strings.Contains(code, "ModelArts.81004") || strings.Contains(code, "ModelArts.81009"):
		// Model rejected
		status = 404
	case strings.Contains(code, "ModelArts.81113"):
		// Payload too large
		status = 400
	case strings.Contains(msg, "blocked") || strings.Contains(msg, "violation") || strings.Contains(msg, "违规"):
		blocked = true
	}

	displayMsg := fmt.Sprintf("OfficeAce 上游请求失败（HTTP %d）", status)
	if code != "" {
		displayMsg += "，业务码 " + code
	}
	if msg != "" && len(msg) < 120 {
		displayMsg += "：" + msg
	}

	return &provider.Error{
		Status:         status,
		Code:           code,
		AccountFault:   fault,
		ContentBlocked: blocked,
		Message:        displayMsg,
	}
}

func parseChunkError(raw json.RawMessage) error {
	var errData map[string]json.RawMessage
	if json.Unmarshal(raw, &errData) == nil {
		code := firstString(errData, "code", "error_code")
		msg := firstString(errData, "message", "error_msg")
		return &provider.Error{
			Status:  502,
			Code:    code,
			Message: "OfficeAce 上游生成错误：" + msg,
		}
	}
	return providerhttp.Broken("OfficeAce")
}
