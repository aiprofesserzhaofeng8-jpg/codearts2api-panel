package requestpolicy

import "strings"

// ExplicitContentBlock matches explicit policy errors, never generic 403,
// device/channel checks, insufficient quota or authentication failures.
// Adapters pass error fields only, not the entire response body.
func ExplicitContentBlock(status int, code, message string) bool {
	if status != 400 && status != 403 {
		return false
	}
	switch strings.ToLower(code) {
	case "content_filter", "content_policy_violation", "content_filter_error":
		return true
	}
	message = strings.ToLower(message)
	for _, marker := range []string{"blocked by security policy", "blocked by content policy", "content policy violation", "content_policy_violation", "内容安全审核未通过", "内容被安全策略拦截"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
