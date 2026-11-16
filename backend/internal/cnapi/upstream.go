// SPDX-License-Identifier: LGPL-3.0-only
// Copyright Wei-Shaw and Sub2API contributors. See UPSTREAM.md.
package cnapi

import "strings"

func cnProviderResponseIndicatesInsufficientBalance(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	s := strings.ToLower(string(body))
	return strings.Contains(s, "余额不足") ||
		strings.Contains(s, "insufficient balance") ||
		strings.Contains(s, "insufficient_credit") ||
		strings.Contains(s, "balance is not enough") ||
		strings.Contains(s, "no enough balance")
}
