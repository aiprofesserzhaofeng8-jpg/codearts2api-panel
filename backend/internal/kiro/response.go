package kiro

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"

	"codearts2api/internal/provider"
)

const maxErrorResponse = 64 << 10

// Some Kiro endpoints label binary EventStream as application/json. Inspect
// the body without buffering the generation; readFrame still validates every CRC.
func convertResponse(ctx context.Context, body io.Reader, model string, emit func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	reader := bufio.NewReader(body)
	prefix, err := reader.Peek(1)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == io.EOF {
			return broken()
		}
		return provider.NewTransportError("Kiro", err)
	}
	// Valid frames start with zero (the existing maximum frame size is 8 MiB).
	// Whitespace belongs to a possible JSON error and counts against its limit.
	if !strings.ContainsRune("{ \t\r\n", rune(prefix[0])) {
		return convertStream(ctx, reader, model, emit)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxErrorResponse+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return provider.NewTransportError("Kiro", err)
	}
	if len(data) > maxErrorResponse {
		return broken()
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil || obj == nil {
		return broken()
	}
	for _, key := range []string{"__type", "code", "errorType", "type"} {
		var code string
		if json.Unmarshal(obj[key], &code) == nil && code != "" {
			return upstreamException(code)
		}
	}
	return broken()
}

// Only recognized machine codes are exposed. Free-form upstream messages and
// unknown codes may contain request or credential data and must stay private.
func upstreamException(raw string) error {
	code := raw
	if i := strings.LastIndexByte(code, '#'); i >= 0 {
		code = code[i+1:]
	}
	if i := strings.IndexByte(code, ':'); i >= 0 {
		code = code[:i]
	}
	status, fault, message := 502, false, "Kiro 上游返回异常"
	switch strings.ToLower(code) {
	case "unauthorizedexception", "unrecognizedclientexception", "expiredtokenexception":
		status, fault, message = 401, true, "Kiro 凭据无效或已过期"
	case "accessdeniedexception":
		status, message = 403, "Kiro 拒绝访问，请检查账号权限"
	case "paymentrequiredexception", "servicequotaexceededexception":
		status, message = 402, "Kiro 配额或订阅限制"
	case "throttlingexception", "toomanyrequestsexception":
		status, message = 429, "Kiro 请求受到限流"
	case "validationexception":
		status, message = 400, "Kiro 拒绝请求参数"
	case "internalserverexception", "serviceunavailableexception":
		status, message = 502, "Kiro 上游服务异常"
	default:
		code = ""
	}
	return &provider.Error{Status: status, AccountFault: fault, Code: code, Message: message}
}
