package codearts

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

var secretKeyValue = regexp.MustCompile(`(?i)(access[_-]?key(?:[_-]?id)?|secret[_-]?(?:access[_-]?)?key|security[_-]?token|refresh[_-]?token|access[_-]?token|code_verifier|client_secret|password|authorization_code)(["']?\s*[:=]\s*["']?)([^\s"',}&]+)`)
var bearerValue = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/-]+`)

// Error text only: never rewrite successful model output or credential payloads.
func RedactSecrets(text string, known ...string) string {
	for _, secret := range known {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
			text = strings.ReplaceAll(text, url.QueryEscape(secret), "[redacted]")
		}
	}
	var v any
	if json.Unmarshal([]byte(text), &v) == nil {
		var walk func(any, int)
		walk = func(v any, depth int) {
			if depth > 32 {
				return
			}
			switch obj := v.(type) {
			case map[string]any:
				for k, value := range obj {
					n := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(k))
					switch n {
					case "accesskey", "accesskeyid", "secretkey", "secretaccesskey", "securitytoken", "refreshtoken", "accesstoken", "codeverifier", "clientsecret", "password", "authorization", "authorizationcode", "dpopprivatejwk", "privatekeyjwk", "d":
						obj[k] = "[redacted]"
					default:
						walk(value, depth+1)
					}
				}
			case []any:
				for _, value := range obj {
					walk(value, depth+1)
				}
			}
		}
		walk(v, 0)
		if b, err := json.Marshal(v); err == nil {
			text = string(b)
		}
	}
	text = secretKeyValue.ReplaceAllString(text, "${1}${2}[redacted]")
	return bearerValue.ReplaceAllString(text, "${1}[redacted]")
}
func sessionErrorText(text string, sess *Session) string {
	if sess == nil {
		return RedactSecrets(text)
	}
	return RedactSecrets(text, sess.AccessKey, sess.SecretKey, sess.SecurityToken, sess.AccessToken, sess.RefreshToken, sess.CodeVerifier)
}
