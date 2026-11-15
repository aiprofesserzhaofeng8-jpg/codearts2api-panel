package providerhttp

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"

	"codearts2api/internal/provider"
)

var diagnosticCode = regexp.MustCompile(`^(?:[A-Z][A-Z0-9_]{0,47}|-?[0-9]{1,10})$`)

// FailureResponseWithCode retains a bounded structured error code, never the
// upstream message, request body, URL, or headers. It does not infer permissions
// or retryability from an unfamiliar code. Callers still close the body.
func FailureResponseWithCode(name string, resp *http.Response) error {
	err := FailureResponse(name, resp).(*provider.Error)
	b, readErr := io.ReadAll(io.LimitReader(resp.Body, (16<<10)+1))
	if readErr != nil || len(b) > 16<<10 {
		return err
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(b, &object) != nil {
		return err
	}
	layers := []map[string]json.RawMessage{object}
	for _, key := range []string{"error", "data"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(object[key], &nested) == nil && nested != nil {
			layers = append(layers, nested)
		}
	}
	for _, layer := range layers {
		for _, key := range []string{"code", "errorCode", "error_code", "responseCode"} {
			raw := layer[key]
			var code string
			if json.Unmarshal(raw, &code) != nil {
				code = string(raw)
			}
			if !diagnosticCode.MatchString(code) || code == "0" || code == "200" || strings.HasPrefix(code, "SK_") {
				continue
			}
			err.Code = code
			err.Message += "，业务码 " + code
			return err
		}
	}
	return err
}
